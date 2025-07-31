package event_publisher

import (
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"go.uber.org/zap"
	"yield/apps/yield/internal/events"
	"yield/apps/yield/internal/model"
	"yield/apps/yield/internal/repository"
)

type EventPublisher struct {
	logger        *zap.Logger
	kafkaProducer *kafka.Producer
	kafkaTopic    string
	repository    *repository.CrawlerRepository
	mu            sync.Mutex // Protects concurrent access to publishing operations
}

func NewEventPublisher(kafkaBroker, kafkaTopic string, logger *zap.Logger, repository *repository.CrawlerRepository) (*EventPublisher, error) {
	// Setup Kafka producer
	producer, err := kafka.NewProducer(&kafka.ConfigMap{
		"bootstrap.servers": kafkaBroker,
		"acks":              "all",
		"retries":           3,
		"retry.backoff.ms":  100,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka producer: %w", err)
	}

	return &EventPublisher{
		logger:        logger,
		kafkaProducer: producer,
		kafkaTopic:    kafkaTopic,
		repository:    repository,
	}, nil
}

func (ep *EventPublisher) StartPublishing() {
	ticker := time.NewTicker(3 * time.Second) // Check every 30 seconds
	defer ticker.Stop()

	for range ticker.C {
		if err := ep.publishUnsentEvents(); err != nil {
			ep.logger.Error("Error publishing events to Kafka", zap.Error(err))
		}
	}
}

func (ep *EventPublisher) publishUnsentEvents() error {
	// Use mutex to ensure only one publishing operation at a time per instance
	ep.mu.Lock()
	defer ep.mu.Unlock()

	// Get unsent events from repository with thread-safe locking
	outboxEvents, err := ep.repository.GetUnsentEventsForProcessing(100)
	if err != nil {
		return err
	}

	// Publish each event to Kafka
	successCount := 0
	for _, event := range outboxEvents {
		if err := ep.publishEventToKafka(event); err != nil {
			ep.logger.Error("Failed to publish event to Kafka", zap.String("tx_hash", event.TxHash), zap.String("event_type", event.EventType), zap.Error(err))
			// Mark as failed (returns status to 'unsent' for retry)
			if markErr := ep.repository.MarkEventAsFailed(event.TxHash, event.EventType, event.LogIndex); markErr != nil {
				ep.logger.Error("Failed to mark event as failed", zap.String("tx_hash", event.TxHash), zap.String("event_type", event.EventType), zap.Uint("log_index", event.LogIndex), zap.Error(markErr))
			}
			continue
		}

		// Mark as sent
		if err := ep.repository.MarkEventAsSent(event.TxHash, event.EventType, event.LogIndex); err != nil {
			ep.logger.Error("Failed to mark event as sent", zap.String("tx_hash", event.TxHash), zap.String("event_type", event.EventType), zap.Uint("log_index", event.LogIndex), zap.Error(err))
			// Note: Event was successfully published but marking failed - this could lead to duplicate sends
		} else {
			successCount++
		}
	}

	if successCount > 0 {
		ep.logger.Info("Published events to Kafka", zap.Int("success_count", successCount), zap.Int("attempted", len(outboxEvents)))
	}

	return nil
}

func (ep *EventPublisher) publishEventToKafka(event model.OutboxEvent) error {
	// Create Kafka message using the structured type
	kafkaMsg := events.TransferEvent{
		EventType:     event.EventType,
		TxHash:        event.TxHash,
		BlockNumber:   event.BlockNumber,
		LogIndex:      uint64(event.LogIndex),
		TxDate:        event.TxDate,
		WalletAddress: event.Address,
		EventData:     event.EventBlob,
		Amount:        event.Amount,
		FromAssetName: event.FromAssetName,
		ToAssetName:   event.ToAssetName,
		Timestamp:     time.Now(),
	}

	msgBytes, err := json.Marshal(kafkaMsg)
	if err != nil {
		return err
	}

	// Publish to Kafka
	deliveryChan := make(chan kafka.Event)
	defer close(deliveryChan)

	err = ep.kafkaProducer.Produce(&kafka.Message{
		TopicPartition: kafka.TopicPartition{Topic: &ep.kafkaTopic, Partition: kafka.PartitionAny},
		Key:            []byte(event.Address), // Use wallet address as key for partition consistency
		Value:          msgBytes,
	}, deliveryChan)

	if err != nil {
		return err
	}

	// Wait for delivery confirmation
	e := <-deliveryChan
	switch ev := e.(type) {
	case *kafka.Message:
		if ev.TopicPartition.Error != nil {
			return ev.TopicPartition.Error
		}
		return nil
	default:
		return fmt.Errorf("unexpected kafka event type: %T", e)
	}
}

func (ep *EventPublisher) Close() error {
	if ep.kafkaProducer != nil {
		ep.kafkaProducer.Close()
	}
	return nil
}
