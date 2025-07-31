package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"yield/apps/yield/internal/repository"
)

// OrderHandler handles order-related API endpoints
type OrderHandler struct {
	orderRepository            *repository.OrderRepository
	monitoredAddressRepository *repository.MonitoredAddressRepository
	transactionBuilder         *TransactionBuilder
	logger                     *zap.Logger
}

// NewOrderHandler creates a new OrderHandler
func NewOrderHandler(orderRepository *repository.OrderRepository, monitoredAddressRepository *repository.MonitoredAddressRepository, rpcURL string, logger *zap.Logger) (*OrderHandler, error) {
	transactionBuilder, err := NewTransactionBuilder(rpcURL)
	if err != nil {
		return nil, err
	}

	return &OrderHandler{
		orderRepository:            orderRepository,
		monitoredAddressRepository: monitoredAddressRepository,
		transactionBuilder:         transactionBuilder,
		logger:                     logger,
	}, nil
}

// GetOrder handles GET /api/orders/{tx_hash}
func (h *OrderHandler) GetOrder(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	txHash := vars["tx_hash"]

	if txHash == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_tx_hash", "Transaction hash is required")
		return
	}

	// Get order from database
	order, err := h.orderRepository.GetOrderByTxHash(txHash)
	if err != nil {
		h.logger.Error("Failed to get order", zap.String("tx_hash", txHash), zap.Error(err))
		h.writeErrorResponse(w, http.StatusInternalServerError, "database_error", "Failed to retrieve order")
		return
	}

	if order == nil {
		h.writeErrorResponse(w, http.StatusNotFound, "order_not_found", "Order not found")
		return
	}

	// Convert to API response
	response := OrderResponse{
		OrderID:         order.OrderID,
		TxHash:          order.TxHash,
		WalletAddress:   order.WalletAddress,
		FromAssetName:   order.FromAssetName,
		ToAssetName:     order.ToAssetName,
		TxDate:          order.TxDate,
		Status:          order.Status,
		TransferType:    order.TransferType,
		Amount:          order.Amount,
		EstimatedAmount: order.EstimatedAmount,
	}

	h.writeJSONResponse(w, http.StatusOK, response)
}

// CreateDeposit handles POST /api/orders/deposit
func (h *OrderHandler) CreateDeposit(w http.ResponseWriter, r *http.Request) {
	var req DepositRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "invalid_request_body", "Invalid JSON in request body")
		return
	}

	// Validate required fields
	if req.Amount == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_amount", "Amount is required")
		return
	}

	if req.FromAssetName == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_from_asset_name", "From asset name is required")
		return
	}

	if req.WalletAddress == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_wallet_address", "Wallet address is required")
		return
	}

	// Normalize asset name to uppercase for validation
	normalizedAssetName := strings.ToUpper(req.FromAssetName)

	// Validate supported asset
	if !h.transactionBuilder.IsAssetSupported(normalizedAssetName) {
		h.writeErrorResponse(w, http.StatusBadRequest, "unsupported_asset", "Asset not supported. Supported assets: LBTC, CBTC, WBTC")
		return
	}

	// Add wallet address to monitored addresses (chain_id = 1 for Ethereum mainnet)
	if err := h.monitoredAddressRepository.AddMonitoredAddress(req.WalletAddress, 1); err != nil {
		h.logger.Error("Failed to add wallet to monitored addresses", zap.Error(err))
		h.writeErrorResponse(w, http.StatusInternalServerError, "monitoring_error", "Failed to add wallet to monitoring")
		return
	}

	// Create unsigned transaction
	unsignedTx, err := h.transactionBuilder.BuildDepositTransaction(normalizedAssetName, req.Amount, req.WalletAddress)
	if err != nil {
		h.logger.Error("Failed to build deposit transaction", zap.Error(err))
		h.writeErrorResponse(w, http.StatusInternalServerError, "transaction_build_error", "Failed to build transaction")
		return
	}

	// Serialize unsigned transaction
	unsignedTxJSON, err := json.Marshal(unsignedTx)
	if err != nil {
		h.logger.Error("Failed to marshal unsigned transaction", zap.Error(err))
		h.writeErrorResponse(w, http.StatusInternalServerError, "serialization_error", "Failed to serialize transaction")
		return
	}

	response := DepositResponse{
		UnsignedTransaction: string(unsignedTxJSON),
	}

	h.logger.Info("Built deposit transaction",
		zap.String("wallet_address", req.WalletAddress),
		zap.String("from_asset", normalizedAssetName),
		zap.String("amount", req.Amount))

	h.writeJSONResponse(w, http.StatusCreated, response)
}

// CreateWithdrawal handles POST /api/orders/withdrawal
func (h *OrderHandler) CreateWithdrawal(w http.ResponseWriter, r *http.Request) {
	var req WithdrawalRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		h.writeErrorResponse(w, http.StatusBadRequest, "invalid_request_body", "Invalid JSON in request body")
		return
	}

	// Validate required fields
	if req.Amount == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_amount", "Amount is required")
		return
	}

	if req.ToAssetName == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_to_asset_name", "To asset name is required")
		return
	}

	if req.WalletAddress == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_wallet_address", "Wallet address is required")
		return
	}

	// Normalize asset name to uppercase for validation
	normalizedAssetName := strings.ToUpper(req.ToAssetName)

	// Validate supported asset (withdrawal target assets)
	if !h.transactionBuilder.IsAssetSupported(normalizedAssetName) {
		h.writeErrorResponse(w, http.StatusBadRequest, "unsupported_asset", "Asset not supported. Supported assets: LBTC, CBTC, WBTC")
		return
	}

	// Add wallet address to monitored addresses (chain_id = 1 for Ethereum mainnet)
	if err := h.monitoredAddressRepository.AddMonitoredAddress(req.WalletAddress, 1); err != nil {
		h.logger.Error("Failed to add wallet to monitored addresses", zap.Error(err))
		h.writeErrorResponse(w, http.StatusInternalServerError, "monitoring_error", "Failed to add wallet to monitoring")
		return
	}

	// Create unsigned transaction for withdrawal
	unsignedTx, err := h.transactionBuilder.BuildWithdrawalTransaction(normalizedAssetName, req.Amount, req.WalletAddress)
	if err != nil {
		h.logger.Error("Failed to build withdrawal transaction", zap.Error(err))
		h.writeErrorResponse(w, http.StatusInternalServerError, "transaction_build_error", "Failed to build transaction")
		return
	}

	// Serialize unsigned transaction
	unsignedTxJSON, err := json.Marshal(unsignedTx)
	if err != nil {
		h.logger.Error("Failed to marshal unsigned transaction", zap.Error(err))
		h.writeErrorResponse(w, http.StatusInternalServerError, "serialization_error", "Failed to serialize transaction")
		return
	}

	response := WithdrawalResponse{
		UnsignedTransaction: string(unsignedTxJSON),
	}

	h.logger.Info("Built withdrawal transaction",
		zap.String("wallet_address", req.WalletAddress),
		zap.String("to_asset", normalizedAssetName),
		zap.String("amount", req.Amount))

	h.writeJSONResponse(w, http.StatusCreated, response)
}

// writeJSONResponse writes a JSON response with the specified status code
func (h *OrderHandler) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", zap.Error(err))
	}
}

// writeErrorResponse writes an error response
func (h *OrderHandler) writeErrorResponse(w http.ResponseWriter, statusCode int, errorCode, message string) {
	errorResponse := ErrorResponse{
		Error:   errorCode,
		Message: message,
	}
	h.writeJSONResponse(w, statusCode, errorResponse)
}
