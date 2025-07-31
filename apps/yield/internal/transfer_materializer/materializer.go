package transfer_materializer

import (
	"encoding/json"
	"fmt"
	"math/big"
	"strings"

	"github.com/confluentinc/confluent-kafka-go/kafka"
	"github.com/google/uuid"
	"go.uber.org/zap"
	"yield/apps/yield/internal/events"
	"yield/apps/yield/internal/model"
	"yield/apps/yield/internal/repository"
)

type TransferMaterializer struct {
	logger          *zap.Logger
	kafkaConsumer   *kafka.Consumer
	orderRepository *repository.OrderRepository
	kafkaTopic      string
}

func NewTransferMaterializer(kafkaBroker, kafkaTopic string, logger *zap.Logger, orderRepository *repository.OrderRepository) (*TransferMaterializer, error) {
	// Setup Kafka consumer
	consumer, err := kafka.NewConsumer(&kafka.ConfigMap{
		"bootstrap.servers": kafkaBroker,
		"group.id":          "transfer-materializer",
		"auto.offset.reset": "earliest",
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create Kafka consumer: %w", err)
	}

	return &TransferMaterializer{
		logger:          logger,
		kafkaConsumer:   consumer,
		orderRepository: orderRepository,
		kafkaTopic:      kafkaTopic,
	}, nil
}

func (tm *TransferMaterializer) Start() error {
	tm.logger.Info("Starting Transfer Materializer...")

	// Subscribe to the topic
	err := tm.kafkaConsumer.Subscribe(tm.kafkaTopic, nil)
	if err != nil {
		return fmt.Errorf("failed to subscribe to topic %s: %w", tm.kafkaTopic, err)
	}

	// Start consuming messages
	for {
		msg, err := tm.kafkaConsumer.ReadMessage(-1)
		if err != nil {
			tm.logger.Error("Error reading message from Kafka", zap.Error(err))
			continue
		}

		if err := tm.processMessage(msg); err != nil {
			tm.logger.Error("Error processing message",
				zap.String("topic", *msg.TopicPartition.Topic),
				zap.Int32("partition", msg.TopicPartition.Partition),
				zap.String("key", string(msg.Key)),
				zap.Error(err))
		}
	}
}

func (tm *TransferMaterializer) processMessage(msg *kafka.Message) error {
	// Parse the Kafka message
	var transferEvent events.TransferEvent
	if err := json.Unmarshal(msg.Value, &transferEvent); err != nil {
		return fmt.Errorf("failed to unmarshal transfer event: %w", err)
	}

	tm.logger.Info("Processing transfer event",
		zap.String("event_type", transferEvent.EventType),
		zap.String("tx_hash", transferEvent.TxHash),
		zap.String("wallet_address", transferEvent.WalletAddress))

	// Handle withdrawal_completed events specially
	if strings.ToLower(transferEvent.EventType) == "withdrawal_completed" {
		return tm.processWithdrawalCompleted(transferEvent)
	}

	// Handle withdrawal_requested events specially
	if strings.ToLower(transferEvent.EventType) == "withdrawal_requested" {
		return tm.processWithdrawalRequested(transferEvent)
	}

	// Map event type to transfer type and status
	transferType, status := tm.mapEventToTransferAndStatus(transferEvent.EventType)

	// Create or update order
	order := model.Order{
		OrderID:         uuid.New().String(),
		TxHash:          transferEvent.TxHash,
		LogIndex:        transferEvent.LogIndex,
		BlockNumber:     transferEvent.BlockNumber,
		TxDate:          transferEvent.TxDate,
		TransferType:    transferType,
		Status:          status,
		WalletAddress:   transferEvent.WalletAddress,
		Amount:          transferEvent.Amount,
		FromAssetName:   transferEvent.FromAssetName,
		ToAssetName:     transferEvent.ToAssetName,
		EstimatedAmount: nil, // For deposit events, estimated_amount remains nil
	}

	return tm.orderRepository.UpsertOrder(order)
}

func (tm *TransferMaterializer) processWithdrawalRequested(transferEvent events.TransferEvent) error {
	// Calculate estimated amount from event data
	estimatedAmount, err := tm.calculateEstimatedAmount(transferEvent.EventData, transferEvent.Amount)
	if err != nil {
		return fmt.Errorf("failed to calculate estimated amount for withdrawal request: %w", err)
	}

	// Check if there's already an in_progress withdrawal for this wallet with the same amount
	existingWithdrawal, err := tm.orderRepository.GetInProgressWithdrawalByWalletAndAmount(transferEvent.WalletAddress, transferEvent.Amount)
	if err != nil {
		return fmt.Errorf("failed to find existing in_progress withdrawal for wallet %s and amount %s: %w", transferEvent.WalletAddress, transferEvent.Amount, err)
	}

	if existingWithdrawal != nil {
		// Update the existing withdrawal with the new transaction details
		existingWithdrawal.TxHash = transferEvent.TxHash
		existingWithdrawal.LogIndex = transferEvent.LogIndex
		existingWithdrawal.BlockNumber = transferEvent.BlockNumber
		existingWithdrawal.TxDate = transferEvent.TxDate
		existingWithdrawal.FromAssetName = transferEvent.FromAssetName
		existingWithdrawal.ToAssetName = transferEvent.ToAssetName
		// Update estimated amount with new calculation
		existingWithdrawal.EstimatedAmount = estimatedAmount

		tm.logger.Info("Updating existing in_progress withdrawal",
			zap.String("wallet_address", transferEvent.WalletAddress),
			zap.String("old_tx_hash", existingWithdrawal.TxHash),
			zap.String("new_tx_hash", transferEvent.TxHash),
			zap.String("amount", transferEvent.Amount))

		return tm.orderRepository.UpsertOrder(*existingWithdrawal)
	}

	// No existing withdrawal found, create a new one
	order := model.Order{
		OrderID:         uuid.New().String(),
		TxHash:          transferEvent.TxHash,
		LogIndex:        transferEvent.LogIndex,
		BlockNumber:     transferEvent.BlockNumber,
		TxDate:          transferEvent.TxDate,
		TransferType:    "withdrawal",
		Status:          "in_progress",
		WalletAddress:   transferEvent.WalletAddress,
		Amount:          transferEvent.Amount,
		FromAssetName:   transferEvent.FromAssetName,
		ToAssetName:     transferEvent.ToAssetName,
		EstimatedAmount: estimatedAmount,
	}

	tm.logger.Info("Creating new withdrawal request",
		zap.String("wallet_address", transferEvent.WalletAddress),
		zap.String("tx_hash", transferEvent.TxHash),
		zap.String("amount", transferEvent.Amount))

	return tm.orderRepository.UpsertOrder(order)
}

func (tm *TransferMaterializer) processWithdrawalCompleted(transferEvent events.TransferEvent) error {
	// Find the last in_progress withdrawal for this wallet
	lastWithdrawal, err := tm.orderRepository.GetLastInProgressWithdrawalByWallet(transferEvent.WalletAddress)
	if err != nil {
		return fmt.Errorf("failed to find last in_progress withdrawal for wallet %s: %w", transferEvent.WalletAddress, err)
	}

	if lastWithdrawal == nil {
		// No in_progress withdrawal found, create a new completed order
		// For withdrawal_completed events, use the amount as estimated_amount if not already set
		estimatedAmount := &transferEvent.Amount

		order := model.Order{
			OrderID:         uuid.New().String(),
			TxHash:          transferEvent.TxHash,
			LogIndex:        transferEvent.LogIndex,
			BlockNumber:     transferEvent.BlockNumber,
			TxDate:          transferEvent.TxDate,
			TransferType:    "withdrawal",
			Status:          "completed",
			WalletAddress:   transferEvent.WalletAddress,
			Amount:          transferEvent.Amount,
			FromAssetName:   transferEvent.FromAssetName,
			ToAssetName:     transferEvent.ToAssetName,
			EstimatedAmount: estimatedAmount,
		}

		tm.logger.Info("Creating new completed withdrawal order",
			zap.String("wallet_address", transferEvent.WalletAddress),
			zap.String("completion_tx_hash", transferEvent.TxHash))

		return tm.orderRepository.UpsertOrder(order)
	}

	// Update the status of the in_progress withdrawal to completed
	// Preserve existing estimated_amount, or use the amount if not set
	if lastWithdrawal.EstimatedAmount == nil {
		lastWithdrawal.EstimatedAmount = &transferEvent.Amount
	}

	lastWithdrawal.Status = "completed"

	if err := tm.orderRepository.UpsertOrder(*lastWithdrawal); err != nil {
		return fmt.Errorf("failed to update withdrawal status to completed: %w", err)
	}

	tm.logger.Info("Marked withdrawal as completed",
		zap.String("wallet_address", transferEvent.WalletAddress),
		zap.String("withdrawal_tx_hash", lastWithdrawal.TxHash),
		zap.String("completion_tx_hash", transferEvent.TxHash))

	return nil
}

func (tm *TransferMaterializer) mapEventToTransferAndStatus(eventType string) (transferType, status string) {
	switch strings.ToLower(eventType) {
	case "deposit":
		return "deposit", "completed"
	case "withdrawal_requested":
		return "withdrawal", "in_progress"
	case "withdrawal_completed":
		return "withdrawal", "completed"
	default:
		tm.logger.Warn("Unknown event type", zap.String("event_type", eventType))
		return eventType, "unknown"
	}
}

func (tm *TransferMaterializer) calculateEstimatedAmount(eventData json.RawMessage, amount string) (*string, error) {
	// Parse the event blob to extract min_price
	var eventMap map[string]interface{}
	if err := json.Unmarshal(eventData, &eventMap); err != nil {
		return nil, fmt.Errorf("failed to unmarshal event data: %w", err)
	}

	minPriceStr, ok := eventMap["min_price"].(string)
	if !ok {
		return nil, fmt.Errorf("min_price not found in event data")
	}

	// Convert strings to big.Float for decimal calculation
	amountFloat, ok := new(big.Float).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("failed to parse amount: %s", amount)
	}

	minPriceFloat, ok := new(big.Float).SetString(minPriceStr)
	if !ok {
		return nil, fmt.Errorf("failed to parse min_price: %s", minPriceStr)
	}

	// Check for division by zero - return nil (NULL) for zero min_price
	if minPriceFloat.Cmp(big.NewFloat(0)) == 0 {
		tm.logger.Warn("min_price is zero, setting estimated_amount to NULL", 
			zap.String("amount", amount),
			zap.String("min_price", minPriceStr))
		return nil, nil
	}

	// Calculate estimated_amount = (min_price × amount) ÷ (10^8)
	// First multiply min_price by amount
	numeratorFloat := new(big.Float).Mul(minPriceFloat, amountFloat)
	
	// Create 10^8 as divisor
	divisorFloat := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(8), nil))
	
	// Divide by 10^8
	estimatedAmountFloat := new(big.Float).Quo(numeratorFloat, divisorFloat)
	estimatedAmountStr := estimatedAmountFloat.Text('f', 18) // Use fixed-point notation with 18 decimal places

	return &estimatedAmountStr, nil
}

func (tm *TransferMaterializer) Close() error {
	if tm.kafkaConsumer != nil {
		return tm.kafkaConsumer.Close()
	}
	return nil
}
