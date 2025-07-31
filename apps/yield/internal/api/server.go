package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"yield/apps/yield/internal/repository"
)

// Server represents the API server
type Server struct {
	orderHandler   *OrderHandler
	balanceHandler *BalanceHandler
	infoHandler    *InfoHandler
	logger         *zap.Logger
	server         *http.Server
}

// NewServer creates a new API server
func NewServer(port int, orderRepository *repository.OrderRepository, monitoredAddressRepository *repository.MonitoredAddressRepository, rpcURL string, logger *zap.Logger) (*Server, error) {
	orderHandler, err := NewOrderHandler(orderRepository, monitoredAddressRepository, rpcURL, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create order handler: %w", err)
	}

	balanceHandler, err := NewBalanceHandler(rpcURL, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create balance handler: %w", err)
	}

	infoHandler, err := NewInfoHandler(rpcURL, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create info handler: %w", err)
	}

	return &Server{
		orderHandler:   orderHandler,
		balanceHandler: balanceHandler,
		infoHandler:    infoHandler,
		logger:         logger,
		server: &http.Server{
			Addr:         fmt.Sprintf(":%d", port),
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 15 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
	}, nil
}

// Start starts the API server
func (s *Server) Start() error {
	router := s.setupRoutes()
	s.server.Handler = router

	s.logger.Info("Starting API server", zap.String("address", s.server.Addr))

	if err := s.server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("failed to start API server: %w", err)
	}

	return nil
}

// Stop stops the API server gracefully
func (s *Server) Stop(ctx context.Context) error {
	s.logger.Info("Stopping API server")
	return s.server.Shutdown(ctx)
}

// setupRoutes configures the API routes
func (s *Server) setupRoutes() *mux.Router {
	router := mux.NewRouter()

	// Add middleware
	router.Use(s.loggingMiddleware)
	router.Use(s.corsMiddleware)

	// API routes
	api := router.PathPrefix("/api").Subrouter()

	// Order endpoints
	api.HandleFunc("/orders/{tx_hash}", s.orderHandler.GetOrder).Methods("GET")
	api.HandleFunc("/orders/deposit", s.orderHandler.CreateDeposit).Methods("POST")
	api.HandleFunc("/orders/withdrawal", s.orderHandler.CreateWithdrawal).Methods("POST")

	// Balance endpoints
	api.HandleFunc("/balance/{wallet_address}", s.balanceHandler.GetBalance).Methods("GET")

	// Info endpoint
	api.HandleFunc("/info", s.infoHandler.GetInfo).Methods("GET")

	// Health check endpoint
	api.HandleFunc("/health", s.healthCheck).Methods("GET")

	return router
}

// loggingMiddleware logs HTTP requests
func (s *Server) loggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()

		// Call the next handler
		next.ServeHTTP(w, r)

		// Log the request
		s.logger.Info("HTTP request",
			zap.String("method", r.Method),
			zap.String("path", r.URL.Path),
			zap.String("remote_addr", r.RemoteAddr),
			zap.Duration("duration", time.Since(start)),
		)
	})
}

// corsMiddleware handles CORS headers
func (s *Server) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// healthCheck handles the health check endpoint
func (s *Server) healthCheck(w http.ResponseWriter, r *http.Request) {
	response := map[string]string{
		"status": "healthy",
		"time":   time.Now().UTC().Format(time.RFC3339),
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		s.logger.Error("Failed to encode health check response", zap.Error(err))
	}
}
