package api

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"strconv"
	"strings"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/ethclient"
	"yield/apps/yield/internal/assets"
)

const (
	// Default gas limit for transactions
	DefaultGasLimit    = "200000"
	WithdrawalGasLimit = "300000"

	// Ethereum mainnet chain ID
	EthereumChainID = "1"
)

// TellerWithMultiAssetSupport ABI for the deposit method
const TellerABI = `[{
	"inputs": [
		{"internalType": "address", "name": "depositAsset", "type": "address"},
		{"internalType": "uint256", "name": "depositAmount", "type": "uint256"},
		{"internalType": "uint256", "name": "minimumMint", "type": "uint256"}
	],
	"name": "deposit",
	"outputs": [
		{"internalType": "uint256", "name": "shares", "type": "uint256"}
	],
	"stateMutability": "nonpayable",
	"type": "function"
}]`

// AtomicRequest ABI for the safeUpdateAtomicRequest method
const AtomicRequestABI = `[{
	"inputs": [
		{"internalType": "address", "name": "offer", "type": "address"},
		{"internalType": "address", "name": "want", "type": "address"},
		{"internalType": "tuple", "name": "userRequest", "type": "tuple", "components": [
			{"internalType": "uint96", "name": "offerAmount", "type": "uint96"},
			{"internalType": "uint64", "name": "deadline", "type": "uint64"},
			{"internalType": "uint88", "name": "atomicPrice", "type": "uint88"},
			{"internalType": "bool", "name": "inSolve", "type": "bool"}
		]},
		{"internalType": "address", "name": "accountant", "type": "address"},
		{"internalType": "uint256", "name": "discount", "type": "uint256"}
	],
	"name": "safeUpdateAtomicRequest",
	"outputs": [],
	"stateMutability": "nonpayable",
	"type": "function"
}]`

// TransactionBuilder handles creation of unsigned Ethereum transactions
type TransactionBuilder struct {
	tellerABI        abi.ABI
	atomicRequestABI abi.ABI
	ethClient        *ethclient.Client
}

// NewTransactionBuilder creates a new transaction builder
func NewTransactionBuilder(rpcURL string) (*TransactionBuilder, error) {
	tellerABI, err := abi.JSON(strings.NewReader(TellerABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse teller ABI: %w", err)
	}

	atomicRequestABI, err := abi.JSON(strings.NewReader(AtomicRequestABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse atomic request ABI: %w", err)
	}

	ethClient, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	return &TransactionBuilder{
		tellerABI:        tellerABI,
		atomicRequestABI: atomicRequestABI,
		ethClient:        ethClient,
	}, nil
}

// BuildDepositTransaction creates an unsigned transaction for depositing assets
func (tb *TransactionBuilder) BuildDepositTransaction(assetName, amount, walletAddress string) (*UnsignedTransaction, error) {
	// Get asset address
	assetAddress, err := tb.getAssetAddress(assetName)
	if err != nil {
		return nil, err
	}

	// Convert decimal amount to proper token units (LBTC uses 8 decimals)
	amountBig, err := tb.convertTo8Decimals(amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount format: %s", amount)
	}

	// Set minimum mint to 0 (no slippage protection as requested)
	minimumMint := big.NewInt(0)

	// Get current nonce from blockchain
	nonce, err := tb.ethClient.PendingNonceAt(context.Background(), common.HexToAddress(walletAddress))
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce from blockchain: %w", err)
	}

	// Get current gas price from blockchain
	gasPrice, err := tb.ethClient.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price from blockchain: %w", err)
	}

	// Encode the function call
	data, err := tb.tellerABI.Pack("deposit", assetAddress, amountBig, minimumMint)
	if err != nil {
		return nil, fmt.Errorf("failed to pack deposit method: %w", err)
	}

	return &UnsignedTransaction{
		To:       assets.TellerContractAddress,
		Data:     "0x" + hex.EncodeToString(data),
		Value:    "0x0", // No ETH value for ERC20 deposits
		GasLimit: DefaultGasLimit,
		GasPrice: "0x" + gasPrice.Text(16),
		ChainID:  EthereumChainID,
		Nonce:    "0x" + strconv.FormatUint(nonce, 16),
	}, nil
}

// getAssetAddress returns the Ethereum address for the given asset name
func (tb *TransactionBuilder) getAssetAddress(assetName string) (common.Address, error) {
	asset, exists := assets.GlobalRegistry.GetBySymbol(strings.ToUpper(assetName))
	if !exists {
		return common.Address{}, fmt.Errorf("unsupported asset: %s", assetName)
	}
	
	// Only allow deposit assets (not LBTCv)
	if asset.Symbol == "LBTCv" {
		return common.Address{}, fmt.Errorf("cannot deposit vault token: %s", assetName)
	}
	
	return asset.Address, nil
}

// GetSupportedAssets returns a list of supported asset names for deposits
func (tb *TransactionBuilder) GetSupportedAssets() []string {
	supported := make([]string, 0)
	for _, asset := range assets.GlobalRegistry.GetAllAsArray() {
		// Only include deposit assets (not LBTCv)
		if asset.Symbol != "LBTCv" {
			supported = append(supported, asset.Symbol)
		}
	}
	return supported
}

// IsAssetSupported checks if the given asset is supported
func (tb *TransactionBuilder) IsAssetSupported(assetName string) bool {
	supportedAssets := tb.GetSupportedAssets()
	assetUpper := strings.ToUpper(assetName)

	for _, asset := range supportedAssets {
		if asset == assetUpper {
			return true
		}
	}
	return false
}

// convertToWei converts a decimal amount string to wei (multiply by 10^18)
func (tb *TransactionBuilder) convertToWei(amount string) (*big.Int, error) {
	// Parse the decimal string
	amountFloat, ok := new(big.Float).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid decimal format")
	}

	// Multiply by 10^18 to convert to wei
	weiMultiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(18), nil))
	weiAmount := new(big.Float).Mul(amountFloat, weiMultiplier)

	// Convert to big.Int (truncate any fractional wei)
	weiInt, _ := weiAmount.Int(nil)
	return weiInt, nil
}

// BuildWithdrawalTransaction creates an unsigned transaction for withdrawing LBTCv assets
func (tb *TransactionBuilder) BuildWithdrawalTransaction(toAssetName, amount, walletAddress string) (*UnsignedTransaction, error) {
	// Get target asset address
	wantAddress, err := tb.getAssetAddress(toAssetName)
	if err != nil {
		return nil, err
	}

	// Offer is always LBTCv
	lbtcvAsset, _ := assets.GlobalRegistry.GetBySymbol("LBTCv")
	offerAddress := lbtcvAsset.Address

	// Convert decimal amount to wei (LBTCv uses 8 decimals like other BTC tokens)
	amountBig, err := tb.convertTo8Decimals(amount)
	if err != nil {
		return nil, fmt.Errorf("invalid amount format: %s", amount)
	}

	// Set deadline to 3 days from now
	deadline := big.NewInt(time.Now().Add(3 * 24 * time.Hour).Unix())

	// Atomic price is 0
	atomicPrice := big.NewInt(0)

	// Accountant address
	accountant := common.HexToAddress(assets.AccountantContractAddress)

	// Discount is 100 (as uint256, not uint16)
	discount := big.NewInt(100)

	// inSolve is false
	inSolve := false

	// Get current nonce from blockchain
	nonce, err := tb.ethClient.PendingNonceAt(context.Background(), common.HexToAddress(walletAddress))
	if err != nil {
		return nil, fmt.Errorf("failed to get nonce from blockchain: %w", err)
	}

	// Get current gas price from blockchain
	gasPrice, err := tb.ethClient.SuggestGasPrice(context.Background())
	if err != nil {
		return nil, fmt.Errorf("failed to get gas price from blockchain: %w", err)
	}

	// Create the userRequest tuple struct with correct types and order for ABI
	userRequest := struct {
		OfferAmount *big.Int // uint96 - maps to *big.Int (first in struct)
		Deadline    uint64   // uint64 - maps to uint64
		AtomicPrice *big.Int // uint88 - maps to *big.Int
		InSolve     bool     // bool - maps to bool
	}{
		OfferAmount: amountBig,
		Deadline:    uint64(deadline.Int64()),
		AtomicPrice: atomicPrice,
		InSolve:     inSolve,
	}

	// Encode the function call
	data, err := tb.atomicRequestABI.Pack("safeUpdateAtomicRequest",
		offerAddress, wantAddress, userRequest, accountant, discount)
	if err != nil {
		return nil, fmt.Errorf("failed to pack safeUpdateAtomicRequest method: %w", err)
	}

	return &UnsignedTransaction{
		To:       assets.AtomicRequestContractAddress,
		Data:     "0x" + hex.EncodeToString(data),
		Value:    "0x0", // No ETH value
		GasLimit: WithdrawalGasLimit,
		GasPrice: "0x" + gasPrice.Text(16),
		ChainID:  EthereumChainID,
		Nonce:    "0x" + strconv.FormatUint(nonce, 16),
	}, nil
}

// convertTo8Decimals converts a decimal amount string to 8 decimal places (for BTC tokens)
func (tb *TransactionBuilder) convertTo8Decimals(amount string) (*big.Int, error) {
	// Parse the decimal string
	amountFloat, ok := new(big.Float).SetString(amount)
	if !ok {
		return nil, fmt.Errorf("invalid decimal format")
	}

	// Multiply by 10^8 to convert to 8 decimal places
	multiplier := new(big.Float).SetInt(new(big.Int).Exp(big.NewInt(10), big.NewInt(8), nil))
	scaledAmount := new(big.Float).Mul(amountFloat, multiplier)

	// Convert to big.Int (truncate any fractional units)
	scaledInt, _ := scaledAmount.Int(nil)
	return scaledInt, nil
}
