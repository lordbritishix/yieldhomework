package api

import (
	"context"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"strings"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/gorilla/mux"
	"go.uber.org/zap"
	"yield/apps/yield/internal/assets"
)

// ERC20 ABI for balanceOf function
const ERC20ABI = `[
	{
		"constant": true,
		"inputs": [{"name": "_owner", "type": "address"}],
		"name": "balanceOf",
		"outputs": [{"name": "balance", "type": "uint256"}],
		"type": "function"
	},
	{
		"constant": true,
		"inputs": [],
		"name": "decimals",
		"outputs": [{"name": "", "type": "uint8"}],
		"type": "function"
	}
]`

// Token configuration
type TokenConfig struct {
	Symbol   string
	Address  string
	Decimals int
}

// BalanceHandler handles balance-related API endpoints
type BalanceHandler struct {
	client        *ethclient.Client
	logger        *zap.Logger
	erc20ABI      abi.ABI
	assetRegistry *assets.AssetRegistry
}

// NewBalanceHandler creates a new BalanceHandler
func NewBalanceHandler(rpcURL string, logger *zap.Logger) (*BalanceHandler, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	parsedABI, err := abi.JSON(strings.NewReader(ERC20ABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse ERC20 ABI: %w", err)
	}

	return &BalanceHandler{
		client:        client,
		logger:        logger,
		erc20ABI:      parsedABI,
		assetRegistry: assets.GlobalRegistry,
	}, nil
}

// GetBalance handles GET /api/balance/{wallet_address}
func (h *BalanceHandler) GetBalance(w http.ResponseWriter, r *http.Request) {
	vars := mux.Vars(r)
	walletAddress := vars["wallet_address"]

	if walletAddress == "" {
		h.writeErrorResponse(w, http.StatusBadRequest, "missing_wallet_address", "Wallet address is required")
		return
	}

	// Validate Ethereum address format
	if !common.IsHexAddress(walletAddress) {
		h.writeErrorResponse(w, http.StatusBadRequest, "invalid_wallet_address", "Invalid Ethereum address format")
		return
	}

	address := common.HexToAddress(walletAddress)
	balances := make(map[string]TokenBalance)

	// Get balance for each supported token
	for symbol, asset := range h.assetRegistry.GetAll() {
		balance, err := h.getTokenBalance(address, asset)
		if err != nil {
			h.logger.Error("Failed to get token balance",
				zap.String("token", asset.Symbol),
				zap.String("address", walletAddress),
				zap.Error(err))
			// Continue with other tokens instead of failing completely
			balances[symbol] = TokenBalance{
				Balance:  "0",
				Symbol:   asset.Symbol,
				Address:  asset.Address.Hex(),
				Decimals: asset.Decimals,
			}
			continue
		}

		balances[symbol] = TokenBalance{
			Balance:  balance,
			Symbol:   asset.Symbol,
			Address:  asset.Address.Hex(),
			Decimals: asset.Decimals,
		}
	}

	response := BalanceResponse{
		WalletAddress: walletAddress,
		Balances:      balances,
	}

	h.logger.Info("Retrieved wallet balances",
		zap.String("wallet_address", walletAddress),
		zap.Int("token_count", len(balances)))

	h.writeJSONResponse(w, http.StatusOK, response)
}

// getTokenBalance retrieves the balance for a specific ERC20 token
func (h *BalanceHandler) getTokenBalance(walletAddress common.Address, asset *assets.Asset) (string, error) {
	tokenAddress := asset.Address

	// Prepare the balanceOf call
	data, err := h.erc20ABI.Pack("balanceOf", walletAddress)
	if err != nil {
		return "", fmt.Errorf("failed to pack balanceOf call: %w", err)
	}

	// Make the call
	result, err := h.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &tokenAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call balanceOf: %w", err)
	}

	// Unpack the result
	var balance *big.Int
	err = h.erc20ABI.UnpackIntoInterface(&balance, "balanceOf", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack balanceOf result: %w", err)
	}

	// Convert to decimal representation
	return h.convertToDecimalAmount(balance, asset.Decimals), nil
}

// convertToDecimalAmount converts wei amount to decimal representation
func (h *BalanceHandler) convertToDecimalAmount(amount *big.Int, decimals int) string {
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	wholePart := new(big.Int).Div(amount, divisor)
	remainder := new(big.Int).Mod(amount, divisor)

	// Format as decimal string
	if remainder.Cmp(big.NewInt(0)) == 0 {
		return wholePart.String()
	} else {
		// Pad remainder with leading zeros to match decimal places
		remainderStr := remainder.String()
		for len(remainderStr) < decimals {
			remainderStr = "0" + remainderStr
		}
		// Remove trailing zeros
		remainderStr = strings.TrimRight(remainderStr, "0")
		if remainderStr == "" {
			return wholePart.String()
		}
		return wholePart.String() + "." + remainderStr
	}
}

// writeJSONResponse writes a JSON response with the specified status code
func (h *BalanceHandler) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", zap.Error(err))
	}
}

// writeErrorResponse writes an error response
func (h *BalanceHandler) writeErrorResponse(w http.ResponseWriter, statusCode int, errorCode, message string) {
	errorResponse := ErrorResponse{
		Error:   errorCode,
		Message: message,
	}
	h.writeJSONResponse(w, statusCode, errorResponse)
}
