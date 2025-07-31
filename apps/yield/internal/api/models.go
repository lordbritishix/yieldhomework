package api

import (
	"time"
)

// OrderResponse represents the API response for order information
type OrderResponse struct {
	OrderID         string     `json:"order_id"`
	TxHash          string     `json:"tx_hash"`
	WalletAddress   string     `json:"wallet_address"`
	FromAssetName   string     `json:"from_asset_name"`
	ToAssetName     string     `json:"to_asset_name"`
	TxDate          time.Time  `json:"tx_date"`
	Status          string     `json:"status"`
	TransferType    string     `json:"transfer_type"`
	Amount          string     `json:"amount"`
	EstimatedAmount *string    `json:"estimated_amount,omitempty"`
}

// DepositRequest represents the request body for creating a deposit order
type DepositRequest struct {
	Amount        string `json:"amount" validate:"required"`
	FromAssetName string `json:"from_asset_name" validate:"required,oneof=LBTC CBTC WBTC"`
	WalletAddress string `json:"wallet_address" validate:"required"`
}

// WithdrawalRequest represents the request body for creating a withdrawal order
type WithdrawalRequest struct {
	Amount        string `json:"amount" validate:"required"`
	ToAssetName   string `json:"to_asset_name" validate:"required,oneof=LBTC WBTC CBTC"`
	WalletAddress string `json:"wallet_address" validate:"required"`
}

// DepositResponse represents the response for a deposit transaction creation
type DepositResponse struct {
	UnsignedTransaction string `json:"unsigned_transaction"`
}

// WithdrawalResponse represents the response for a withdrawal transaction creation
type WithdrawalResponse struct {
	UnsignedTransaction string `json:"unsigned_transaction"`
}

// UnsignedTransaction represents the unsigned Ethereum transaction data
type UnsignedTransaction struct {
	To       string `json:"to"`
	Data     string `json:"data"`
	Value    string `json:"value"`
	GasLimit string `json:"gas_limit"`
	GasPrice string `json:"gas_price"`
	ChainID  string `json:"chain_id"`
	Nonce    string `json:"nonce"`
}

// BalanceResponse represents the API response for wallet balance information
type BalanceResponse struct {
	WalletAddress string                    `json:"wallet_address"`
	Balances      map[string]TokenBalance   `json:"balances"`
}

// TokenBalance represents balance information for a specific token
type TokenBalance struct {
	Balance     string `json:"balance"`
	Symbol      string `json:"symbol"`
	Address     string `json:"address"`
	Decimals    int    `json:"decimals"`
}

// InfoResponse represents the API response for vault information
type InfoResponse struct {
	APY         string `json:"apy"`
	TVL         string `json:"tvl"`
	TokenSymbol string `json:"token_symbol"`
	Decimals    int    `json:"decimals"`
	VaultName   string `json:"vault_name"`
}

// ErrorResponse represents the API error response
type ErrorResponse struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}