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
	"go.uber.org/zap"
	"yield/apps/yield/internal/assets"
)

// Vault ABI for fetching vault information - using actual Lombard vault functions
const VaultABI = `[
	{
		"inputs": [],
		"name": "totalSupply",
		"outputs": [{"internalType": "uint256", "name": "", "type": "uint256"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "decimals",
		"outputs": [{"internalType": "uint8", "name": "", "type": "uint8"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "symbol",
		"outputs": [{"internalType": "string", "name": "", "type": "string"}],
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [],
		"name": "name",
		"outputs": [{"internalType": "string", "name": "", "type": "string"}],
		"stateMutability": "view",
		"type": "function"
	}
]`

// Accountant ABI for fetching rate information (APY calculation)
const AccountantABI = `[
	{
		"constant": true,
		"inputs": [],
		"name": "getRate",
		"outputs": [{"name": "", "type": "uint256"}],
		"type": "function"
	}
]`

// InfoHandler handles vault information API endpoints
type InfoHandler struct {
	client            *ethclient.Client
	logger            *zap.Logger
	vaultABI          abi.ABI
	accountantABI     abi.ABI
	vaultAddress      common.Address
	accountantAddress common.Address
}

// NewInfoHandler creates a new InfoHandler
func NewInfoHandler(rpcURL string, logger *zap.Logger) (*InfoHandler, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	parsedVaultABI, err := abi.JSON(strings.NewReader(VaultABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse vault ABI: %w", err)
	}

	parsedAccountantABI, err := abi.JSON(strings.NewReader(AccountantABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse accountant ABI: %w", err)
	}

	// Get vault address from asset registry
	lbtcvAsset, exists := assets.GlobalRegistry.GetBySymbol("LBTCv")
	if !exists {
		return nil, fmt.Errorf("LBTCv asset not found in registry")
	}

	return &InfoHandler{
		client:            client,
		logger:            logger,
		vaultABI:          parsedVaultABI,
		accountantABI:     parsedAccountantABI,
		vaultAddress:      lbtcvAsset.Address,
		accountantAddress: common.HexToAddress(assets.AccountantContractAddress),
	}, nil
}

// GetInfo handles GET /api/info
func (h *InfoHandler) GetInfo(w http.ResponseWriter, r *http.Request) {
	// Fetch vault information concurrently
	tvlChan := make(chan string, 1)
	symbolChan := make(chan string, 1)
	decimalsChan := make(chan int, 1)
	nameChan := make(chan string, 1)
	apyChan := make(chan string, 1)
	errorChan := make(chan error, 5)

	// Get Total Value Locked (TVL)
	go func() {
		tvl, err := h.getTotalAssets()
		if err != nil {
			errorChan <- fmt.Errorf("failed to get TVL: %w", err)
			return
		}
		tvlChan <- tvl
	}()

	// Get token symbol
	go func() {
		symbol, err := h.getSymbol()
		if err != nil {
			errorChan <- fmt.Errorf("failed to get symbol: %w", err)
			return
		}
		symbolChan <- symbol
	}()

	// Get token decimals
	go func() {
		decimals, err := h.getDecimals()
		if err != nil {
			errorChan <- fmt.Errorf("failed to get decimals: %w", err)
			return
		}
		decimalsChan <- decimals
	}()

	// Get vault name
	go func() {
		name, err := h.getName()
		if err != nil {
			errorChan <- fmt.Errorf("failed to get name: %w", err)
			return
		}
		nameChan <- name
	}()

	// Get APY (from rate) - with fallback
	go func() {
		apy, err := h.getAPY()
		if err != nil {
			// Log error but provide fallback value instead of failing
			h.logger.Warn("Failed to get APY from accountant contract, using fallback", zap.Error(err))
			apyChan <- "0.00" // Fallback APY
			return
		}
		apyChan <- apy
	}()

	// Collect results
	var tvl, symbol, name, apy string
	var decimals int
	var errors []error

	for i := 0; i < 5; i++ {
		select {
		case tvl = <-tvlChan:
		case symbol = <-symbolChan:
		case decimals = <-decimalsChan:
		case name = <-nameChan:
		case apy = <-apyChan:
		case err := <-errorChan:
			errors = append(errors, err)
		}
	}

	// If any errors occurred, return error response
	if len(errors) > 0 {
		h.logger.Error("Failed to fetch vault info", zap.Errors("errors", errors))
		h.writeErrorResponse(w, http.StatusInternalServerError, "fetch_error", "Failed to fetch vault information")
		return
	}

	response := InfoResponse{
		APY:         apy,
		TVL:         tvl,
		TokenSymbol: symbol,
		Decimals:    decimals,
		VaultName:   name,
	}

	h.logger.Info("Retrieved vault info",
		zap.String("apy", apy),
		zap.String("tvl", tvl),
		zap.String("symbol", symbol),
		zap.Int("decimals", decimals),
		zap.String("name", name))

	h.writeJSONResponse(w, http.StatusOK, response)
}

// getTotalAssets retrieves the total supply (TVL) from the vault
func (h *InfoHandler) getTotalAssets() (string, error) {
	data, err := h.vaultABI.Pack("totalSupply")
	if err != nil {
		return "", fmt.Errorf("failed to pack totalSupply call: %w", err)
	}

	result, err := h.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &h.vaultAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call totalSupply: %w", err)
	}

	var totalSupply *big.Int
	err = h.vaultABI.UnpackIntoInterface(&totalSupply, "totalSupply", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack totalSupply result: %w", err)
	}

	// Convert to decimal representation (assuming 8 decimals for BTC-based assets)
	return h.convertToDecimalAmount(totalSupply, 8), nil
}

// getSymbol retrieves the token symbol from the vault
func (h *InfoHandler) getSymbol() (string, error) {
	data, err := h.vaultABI.Pack("symbol")
	if err != nil {
		return "", fmt.Errorf("failed to pack symbol call: %w", err)
	}

	result, err := h.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &h.vaultAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call symbol: %w", err)
	}

	var symbol string
	err = h.vaultABI.UnpackIntoInterface(&symbol, "symbol", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack symbol result: %w", err)
	}

	return symbol, nil
}

// getDecimals retrieves the token decimals from the vault
func (h *InfoHandler) getDecimals() (int, error) {
	data, err := h.vaultABI.Pack("decimals")
	if err != nil {
		return 0, fmt.Errorf("failed to pack decimals call: %w", err)
	}

	result, err := h.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &h.vaultAddress,
		Data: data,
	}, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to call decimals: %w", err)
	}

	var decimals uint8
	err = h.vaultABI.UnpackIntoInterface(&decimals, "decimals", result)
	if err != nil {
		return 0, fmt.Errorf("failed to unpack decimals result: %w", err)
	}

	return int(decimals), nil
}

// getName retrieves the vault name
func (h *InfoHandler) getName() (string, error) {
	data, err := h.vaultABI.Pack("name")
	if err != nil {
		return "", fmt.Errorf("failed to pack name call: %w", err)
	}

	result, err := h.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &h.vaultAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call name: %w", err)
	}

	var name string
	err = h.vaultABI.UnpackIntoInterface(&name, "name", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack name result: %w", err)
	}

	return name, nil
}

// getAPY retrieves and calculates APY from the accountant contract
func (h *InfoHandler) getAPY() (string, error) {
	data, err := h.accountantABI.Pack("getRate")
	if err != nil {
		return "", fmt.Errorf("failed to pack getRate call: %w", err)
	}

	result, err := h.client.CallContract(context.Background(), ethereum.CallMsg{
		To:   &h.accountantAddress,
		Data: data,
	}, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call getRate: %w", err)
	}

	var rate *big.Int
	err = h.accountantABI.UnpackIntoInterface(&rate, "getRate", result)
	if err != nil {
		return "", fmt.Errorf("failed to unpack getRate result: %w", err)
	}

	// Convert rate to APY percentage
	// The rate is typically returned in basis points or a similar format
	// We'll convert to percentage with 2 decimal places
	// Assuming rate is in basis points (1 basis point = 0.01%)
	apy := h.convertRateToAPY(rate)
	return apy, nil
}

// convertRateToAPY converts a rate to APY percentage string
func (h *InfoHandler) convertRateToAPY(rate *big.Int) string {
	// Convert rate to percentage
	// Assuming the rate is already an annual rate in some form
	// We'll divide by 100 to get percentage and format to 2 decimal places
	rateDivisor := big.NewInt(10000) // For basis points to percentage conversion
	apy := new(big.Float).SetInt(rate)
	apy.Quo(apy, new(big.Float).SetInt(rateDivisor))

	return fmt.Sprintf("%.2f", apy)
}

// convertToDecimalAmount converts wei amount to decimal representation
func (h *InfoHandler) convertToDecimalAmount(amount *big.Int, decimals int) string {
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
func (h *InfoHandler) writeJSONResponse(w http.ResponseWriter, statusCode int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(statusCode)

	if err := json.NewEncoder(w).Encode(data); err != nil {
		h.logger.Error("Failed to encode JSON response", zap.Error(err))
	}
}

// writeErrorResponse writes an error response
func (h *InfoHandler) writeErrorResponse(w http.ResponseWriter, statusCode int, errorCode, message string) {
	errorResponse := ErrorResponse{
		Error:   errorCode,
		Message: message,
	}
	h.writeJSONResponse(w, statusCode, errorResponse)
}
