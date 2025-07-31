package main

import (
	"context"
	"database/sql"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/lib/pq"
	"go.uber.org/zap"
	"yield/apps/yield/internal/api"
	"yield/apps/yield/internal/config"
	crawler2 "yield/apps/yield/internal/crawler"
	"yield/apps/yield/internal/event_publisher"
	"yield/apps/yield/internal/repository"
	"yield/apps/yield/internal/transfer_materializer"
)

// Main function example
func main() {
	// Initialize zap logger
	logger, err := zap.NewProduction()
	if err != nil {
		panic("Failed to initialize logger: " + err.Error())
	}
	defer logger.Sync()

	// Load configuration from environment variables
	cfg := config.NewConfig()

	// Clear Kafka logs and reset consumers for testing
	//clearKafkaLogs(cfg.KafkaBroker, cfg.KafkaTopic, logger)
	//resetKafkaConsumers(cfg.KafkaBroker, logger)

	logger.Info("Starting application with configuration",
		zap.String("rpc_url", cfg.RpcURL),
		zap.String("db_url", cfg.DbURL),
		zap.String("kafka_broker", cfg.KafkaBroker),
		zap.String("kafka_topic", cfg.KafkaTopic),
		zap.Uint64("chunk_size", cfg.ChunkSize),
		zap.Uint64("finality_offset", cfg.FinalityOffset),
		zap.Int("api_port", cfg.APIPort),
	)

	// Connect to database
	db, err := sql.Open("postgres", cfg.DbURL)
	if err != nil {
		logger.Fatal("Failed to connect to database", zap.Error(err))
	}

	// Initialize database tables
	if err := repository.InitMigration(db); err != nil {
		logger.Fatal("Failed to initialize database", zap.Error(err))
	}

	crawlerRepository := repository.NewCrawlerRepository(db, logger)
	orderRepository := repository.NewOrderRepository(db, logger)
	monitoredAddressRepository := repository.NewMonitoredAddressRepository(db, logger)

	// Create event publisher
	eventPublisher, err := event_publisher.NewEventPublisher(cfg.KafkaBroker, cfg.KafkaTopic, logger, crawlerRepository)
	if err != nil {
		logger.Fatal("Failed to create event publisher", zap.Error(err))
	}
	defer eventPublisher.Close()

	// Start event publisher in background
	go eventPublisher.StartPublishing()

	// Create transfer materializer
	materializer, err := transfer_materializer.NewTransferMaterializer(cfg.KafkaBroker, cfg.KafkaTopic, logger, orderRepository)
	if err != nil {
		logger.Fatal("Failed to create transfer materializer", zap.Error(err))
	}
	defer materializer.Close()

	// Start transfer materializer in background
	go func() {
		if err := materializer.Start(); err != nil {
			logger.Fatal("Transfer materializer failed", zap.Error(err))
		}
	}()

	// Create and start API server
	apiServer, err := api.NewServer(cfg.APIPort, orderRepository, monitoredAddressRepository, cfg.RpcURL, logger)
	if err != nil {
		logger.Fatal("Failed to create API server", zap.Error(err))
	}
	go func() {
		if err := apiServer.Start(); err != nil {
			logger.Fatal("API server failed", zap.Error(err))
		}
	}()

	// Create crawler
	crawler, err := crawler2.NewLombardCrawler(cfg, db, logger, crawlerRepository, monitoredAddressRepository)
	if err != nil {
		logger.Fatal("Failed to create crawler", zap.Error(err))
	}
	defer crawler.Close()

	// Start crawler in background
	go func() {
		if err := crawler.Start(); err != nil {
			logger.Fatal("Crawler failed", zap.Error(err))
		}
	}()

	// Set up signal handling for graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Wait for shutdown signal
	<-sigChan
	logger.Info("Received shutdown signal, starting graceful shutdown...")

	// Create a context with timeout for shutdown
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Shutdown API server gracefully
	if err := apiServer.Stop(ctx); err != nil {
		logger.Error("Error shutting down API server", zap.Error(err))
	}

	logger.Info("Application shutdown complete")
}
