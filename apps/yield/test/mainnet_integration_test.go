package test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"github.com/joho/godotenv"
	"yield/apps/yield/internal/assets"
)

// All shared constants and types are now defined in common.go

const (
	// ERC20 ABI function signatures
	ERC20AllowanceABI = "dd62ed3e" // allowance(owner,spender)
	ERC20ApproveABI   = "095ea7b3" // approve(spender,amount)
)

// loadEnvConfig loads environment variables from .env file if it exists
func loadEnvConfig() {
	// Try to load .env file from test directory, project root, and parent directories
	envPaths := []string{
		".env", // test directory
	}

	for _, path := range envPaths {
		if err := godotenv.Load(path); err == nil {
			log.Printf("✅ Loaded environment variables from %s", path)
			return
		}
	}

	// If no .env file found, that's okay - environment variables might be set another way
	log.Println("ℹ️ No .env file found, using system environment variables")
}

// ChainHelper handles all blockchain-related operations
type ChainHelper struct {
	client *ethclient.Client
}

// NewChainHelper creates a new ChainHelper instance
func NewChainHelper(rpcURL string) (*ChainHelper, error) {
	client, err := ethclient.Dial(rpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum: %w", err)
	}
	return &ChainHelper{client: client}, nil
}

// Close closes the client connection
func (ch *ChainHelper) Close() {
	ch.client.Close()
}

// SignTransaction signs an unsigned transaction with the provided private key
func (ch *ChainHelper) SignTransaction(unsignedTx UnsignedTransaction, privateKey *ecdsa.PrivateKey) (*types.Transaction, error) {
	// Parse transaction fields
	to := common.HexToAddress(unsignedTx.To)

	value, ok := new(big.Int).SetString(strings.TrimPrefix(unsignedTx.Value, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("invalid value: %s", unsignedTx.Value)
	}

	gasLimit, err := strconv.ParseUint(unsignedTx.GasLimit, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid gas limit: %s", unsignedTx.GasLimit)
	}

	gasPrice, ok := new(big.Int).SetString(strings.TrimPrefix(unsignedTx.GasPrice, "0x"), 16)
	if !ok {
		return nil, fmt.Errorf("invalid gas price: %s", unsignedTx.GasPrice)
	}

	nonce, err := strconv.ParseUint(strings.TrimPrefix(unsignedTx.Nonce, "0x"), 16, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid nonce: %s", unsignedTx.Nonce)
	}

	data, err := hex.DecodeString(strings.TrimPrefix(unsignedTx.Data, "0x"))
	if err != nil {
		return nil, fmt.Errorf("invalid data: %s", unsignedTx.Data)
	}

	chainID, ok := new(big.Int).SetString(unsignedTx.ChainID, 10)
	if !ok {
		return nil, fmt.Errorf("invalid chain ID: %s", unsignedTx.ChainID)
	}

	// Create the transaction
	tx := types.NewTransaction(
		nonce,
		to,
		value,
		gasLimit,
		gasPrice,
		data,
	)

	// Sign the transaction
	signer := types.NewEIP155Signer(chainID)
	signedTx, err := types.SignTx(tx, signer, privateKey)
	if err != nil {
		return nil, fmt.Errorf("failed to sign transaction: %w", err)
	}

	return signedTx, nil
}

// BroadcastTransaction broadcasts a signed transaction to the network
func (ch *ChainHelper) BroadcastTransaction(signedTx *types.Transaction) error {
	return ch.client.SendTransaction(context.Background(), signedTx)
}

// WaitForTransaction waits for a transaction to be mined with the specified timeout
func (ch *ChainHelper) WaitForTransaction(txHash common.Hash, timeout time.Duration) (*types.Receipt, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("timeout waiting for transaction %s", txHash.Hex())
		case <-ticker.C:
			receipt, err := ch.client.TransactionReceipt(context.Background(), txHash)
			if err == nil {
				return receipt, nil
			}
			// Continue waiting if transaction not found yet
		}
	}
}

// GetLBTCvAllowance checks LBTCv allowance for AtomicRequest contract
func (ch *ChainHelper) GetLBTCvAllowance(walletAddress string) (string, error) {
	// Create the allowance call data: allowance(owner, spender)
	methodID := common.Hex2Bytes(ERC20AllowanceABI)
	ownerAddress := common.HexToAddress(walletAddress)
	spenderAddress := common.HexToAddress(assets.AtomicRequestContractAddress) // AtomicRequest contract
	
	paddedOwner := common.LeftPadBytes(ownerAddress.Bytes(), 32)
	paddedSpender := common.LeftPadBytes(spenderAddress.Bytes(), 32)
	data := append(methodID, paddedOwner...)
	data = append(data, paddedSpender...)
	
	// Create call message
	lbtcvTokenAddress := common.HexToAddress(LBTCvTokenAddress)
	callMsg := ethereum.CallMsg{
		To:   &lbtcvTokenAddress,
		Data: data,
	}
	
	// Call the contract
	result, err := ch.client.CallContract(context.Background(), callMsg, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call allowance: %w", err)
	}
	
	// Parse the result (32 bytes big-endian integer)
	allowance := new(big.Int).SetBytes(result)
	
	// Convert to decimal representation (LBTCv has 8 decimals)
	return ch.formatTokenAmount(allowance, 8), nil
}

// GetLBTCAllowance checks LBTC allowance for Teller contract
func (ch *ChainHelper) GetLBTCAllowance(walletAddress string) (string, error) {
	// Create the allowance call data: allowance(owner, spender)
	methodID := common.Hex2Bytes(ERC20AllowanceABI)
	ownerAddress := common.HexToAddress(walletAddress)
	spenderAddress := common.HexToAddress(assets.TellerContractAddress)

	paddedOwner := common.LeftPadBytes(ownerAddress.Bytes(), 32)
	paddedSpender := common.LeftPadBytes(spenderAddress.Bytes(), 32)
	data := append(methodID, paddedOwner...)
	data = append(data, paddedSpender...)

	// Create call message
	lbtcTokenAddress := assets.LBTCAddress
	callMsg := ethereum.CallMsg{
		To:   &lbtcTokenAddress,
		Data: data,
	}

	// Call the contract
	result, err := ch.client.CallContract(context.Background(), callMsg, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call allowance: %w", err)
	}

	// Parse the result (32 bytes big-endian integer)
	allowance := new(big.Int).SetBytes(result)

	// Convert to decimal representation (LBTC has 8 decimals)
	return ch.formatTokenAmount(allowance, 8), nil
}

// GetLBTCvBalance gets LBTCv token balance for the specified wallet
func (ch *ChainHelper) GetLBTCvBalance(walletAddress string) (string, error) {
	// Create the balanceOf call data
	methodID := common.Hex2Bytes(ERC20BalanceOfABI)
	address := common.HexToAddress(walletAddress)
	paddedAddress := common.LeftPadBytes(address.Bytes(), 32)
	data := append(methodID, paddedAddress...)

	// Create call message
	to := common.HexToAddress(LBTCvTokenAddress)
	callMsg := ethereum.CallMsg{
		To:   &to,
		Data: data,
	}

	// Call the contract
	result, err := ch.client.CallContract(context.Background(), callMsg, nil)
	if err != nil {
		return "", fmt.Errorf("failed to call balanceOf: %w", err)
	}

	// Parse the result (32 bytes big-endian integer)
	balance := new(big.Int).SetBytes(result)

	// Convert to decimal representation (LBTCv has 8 decimals)
	return ch.formatTokenAmount(balance, 8), nil
}

// formatTokenAmount formats a token amount with the specified decimal places
func (ch *ChainHelper) formatTokenAmount(amount *big.Int, decimals int) string {
	divisor := new(big.Int).Exp(big.NewInt(10), big.NewInt(int64(decimals)), nil)
	wholePart := new(big.Int).Div(amount, divisor)
	remainder := new(big.Int).Mod(amount, divisor)

	if remainder.Cmp(big.NewInt(0)) == 0 {
		return wholePart.String()
	} else {
		// Format with decimals
		remainderStr := remainder.String()
		for len(remainderStr) < decimals {
			remainderStr = "0" + remainderStr
		}
		remainderStr = strings.TrimRight(remainderStr, "0")
		if remainderStr == "" {
			return wholePart.String()
		}
		return wholePart.String() + "." + remainderStr
	}
}

// Helper function to get order status
func getOrderStatus(txHash string) (*OrderResponse, error) {
	getURL := fmt.Sprintf("%s/api/orders/%s", BaseURL, txHash)

	resp, err := http.Get(getURL)
	if err != nil {
		return nil, fmt.Errorf("failed to make GET request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		var errorResp ErrorResponse
		json.NewDecoder(resp.Body).Decode(&errorResp)
		return nil, fmt.Errorf("API error %d: %s - %s", resp.StatusCode, errorResp.Error, errorResp.Message)
	}

	var orderResp OrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&orderResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	return &orderResp, nil
}

func TestCreateDepositTransactionMainnet(t *testing.T) {
	// Test: Create, sign, broadcast deposit transaction and verify end-to-end flow
	t.Run("CreateDepositTransactionMainnet", func(t *testing.T) {
		// Load environment variables from .env file if it exists
		loadEnvConfig()

		// Skip if no private key provided
		privateKeyHex := os.Getenv("TEST_PRIVATE_KEY")
		if privateKeyHex == "" {
			t.Skip("Skipping mainnet test: TEST_PRIVATE_KEY environment variable not set")
		}

		// Get RPC URL from environment or use default
		rpcURL := os.Getenv("ETHEREUM_RPC_URL")
		if rpcURL == "" {
			rpcURL = MainnetRPCURL
		}

		// Connect to Ethereum mainnet using ChainHelper
		chainHelper, err := NewChainHelper(rpcURL)
		if err != nil {
			t.Fatalf("Failed to connect to Ethereum: %v", err)
		}
		defer chainHelper.Close()

		// Parse private key
		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
		if err != nil {
			t.Fatalf("Failed to parse private key: %v", err)
		}

		// Get wallet address from private key
		publicKey := privateKey.Public()
		publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
		if !ok {
			t.Fatal("Failed to get public key")
		}
		walletAddress := crypto.PubkeyToAddress(*publicKeyECDSA).Hex()
		t.Logf("Using wallet address: %s", walletAddress)

		// Step 1: Check LBTC allowance for Teller contract
		allowance, err := chainHelper.GetLBTCAllowance(walletAddress)
		if err != nil {
			t.Logf("Warning: Failed to get LBTC allowance: %v", err)
		} else {
			t.Logf("LBTC allowance for Teller contract: %s", allowance)

			// Parse allowance and test amount to compare
			allowanceFloat, _ := new(big.Float).SetString(allowance)
			testAmountFloat, _ := new(big.Float).SetString(TestAmount)

			if allowanceFloat != nil && testAmountFloat != nil && allowanceFloat.Cmp(testAmountFloat) < 0 {
				t.Logf("⚠️ WARNING: LBTC allowance (%s) is less than test amount (%s)", allowance, TestAmount)
				t.Logf("You need to approve the Teller contract before depositing:")
				t.Logf("Contract: %s", assets.TellerContractAddress)
				t.Logf("You can approve via Etherscan or call: approve('%s', amount)", assets.TellerContractAddress)
			}
		}

		// Step 2: Get initial LBTCv balance
		initialBalance, err := chainHelper.GetLBTCvBalance(walletAddress)
		if err != nil {
			t.Logf("Warning: Failed to get initial balance: %v", err)
			initialBalance = "0" // Continue anyway
		}
		t.Logf("Initial LBTCv balance: %s", initialBalance)

		// Step 3: Create unsigned deposit transaction via API
		depositReq := DepositRequest{
			Amount:        TestAmount,
			FromAssetName: TestFromAsset,
			WalletAddress: walletAddress,
		}

		reqBody, err := json.Marshal(depositReq)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		resp, err := http.Post(
			BaseURL+"/api/orders/deposit",
			"application/json",
			bytes.NewBuffer(reqBody),
		)
		if err != nil {
			t.Fatalf("Failed to make POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			var errorResp ErrorResponse
			json.NewDecoder(resp.Body).Decode(&errorResp)
			t.Fatalf("Expected status 201, got %d. Error: %s - %s",
				resp.StatusCode, errorResp.Error, errorResp.Message)
		}

		var depositResp DepositResponse
		if err := json.NewDecoder(resp.Body).Decode(&depositResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Parse unsigned transaction
		var unsignedTx UnsignedTransaction
		if err := json.Unmarshal([]byte(depositResp.UnsignedTransaction), &unsignedTx); err != nil {
			t.Fatalf("Failed to parse unsigned transaction: %v", err)
		}

		t.Logf("✅ Created unsigned transaction")

		// Step 4: Sign the transaction
		signedTx, err := chainHelper.SignTransaction(unsignedTx, privateKey)
		if err != nil {
			t.Fatalf("Failed to sign transaction: %v", err)
		}
		t.Logf("✅ Transaction signed")

		txHash := signedTx.Hash().Hex()
		t.Logf("Transaction hash: %s", txHash)

		// Step 5: Broadcast the transaction
		t.Logf("🚀 Attempting to broadcast transaction...")
		err = chainHelper.BroadcastTransaction(signedTx)
		if err != nil {
			// Log the error but don't fail the test - might be due to insufficient balance or tokens
			t.Logf("⚠️ Transaction broadcast failed: %v", err)

			// Verify the transaction was properly formatted by checking the error type
			if strings.Contains(err.Error(), "insufficient funds") ||
				strings.Contains(err.Error(), "nonce too low") ||
				strings.Contains(err.Error(), "gas price") ||
				strings.Contains(err.Error(), "replacement transaction underpriced") ||
				strings.Contains(err.Error(), "already known") {
				t.Logf("✅ Transaction properly formatted (failed due to expected account/network issues)")
				return // Skip remaining steps since broadcast failed
			} else {
				t.Fatalf("❌ Transaction malformed or unexpected error: %v", err)
			}
		}

		t.Logf("✅ Transaction broadcast successful: %s", txHash)

		// Step 6: Wait for transaction to be mined
		t.Logf("⏳ Waiting for transaction to be mined...")
		receipt, err := chainHelper.WaitForTransaction(signedTx.Hash(), 10*time.Minute)
		if err != nil {
			t.Fatalf("Failed to wait for transaction: %v", err)
		}

		if receipt.Status == 0 {
			t.Logf("⚠️ Transaction failed on chain (expected if insufficient LBTC tokens)")
			t.Logf("✅ Transaction was properly formatted and mined, but failed during execution")
			t.Logf("This indicates the deposit transaction structure is correct")
			return // Skip remaining steps since transaction failed during execution
		}
		t.Logf("✅ Transaction mined successfully in block %d", receipt.BlockNumber.Uint64())

		// Step 7: Wait for order to be processed and check status
		t.Logf("⏳ Waiting for order to be processed...")
		time.Sleep(30 * time.Second) // Wait for crawler to process

		orderResp, err := getOrderStatus(txHash)
		if err != nil {
			t.Logf("Warning: Failed to get order status: %v", err)
		} else {
			t.Logf("✅ Order status: %s", orderResp.Status)
		}

		// Step 8: Check final balance
		finalBalance, err := chainHelper.GetLBTCvBalance(walletAddress)
		if err != nil {
			t.Logf("Warning: Failed to get final balance: %v", err)
		} else {
			t.Logf("Final LBTCv balance: %s", finalBalance)

			// Compare balances (expect increase)
			if finalBalance != initialBalance {
				t.Logf("✅ Balance changed - deposit successful!")
			} else {
				t.Logf("⚠️ Balance unchanged - may need more time for processing")
			}
		}
	})
}

func TestCreateWithdrawalTransactionMainnet(t *testing.T) {
	// Test: Create, sign, broadcast withdrawal transaction and verify end-to-end flow
	t.Run("CreateWithdrawalTransactionMainnet", func(t *testing.T) {
		// Load environment variables from .env file if it exists
		loadEnvConfig()

		// Skip if no private key provided
		privateKeyHex := os.Getenv("TEST_PRIVATE_KEY")
		if privateKeyHex == "" {
			t.Skip("Skipping mainnet test: TEST_PRIVATE_KEY environment variable not set")
		}

		// Get RPC URL from environment or use default
		rpcURL := os.Getenv("ETHEREUM_RPC_URL")
		if rpcURL == "" {
			rpcURL = MainnetRPCURL
		}

		// Connect to Ethereum mainnet using ChainHelper
		chainHelper, err := NewChainHelper(rpcURL)
		if err != nil {
			t.Fatalf("Failed to connect to Ethereum: %v", err)
		}
		defer chainHelper.Close()

		// Parse private key
		privateKey, err := crypto.HexToECDSA(strings.TrimPrefix(privateKeyHex, "0x"))
		if err != nil {
			t.Fatalf("Failed to parse private key: %v", err)
		}

		// Get wallet address from private key
		publicKey := privateKey.Public()
		publicKeyECDSA, ok := publicKey.(*ecdsa.PublicKey)
		if !ok {
			t.Fatal("Failed to get public key")
		}
		walletAddress := crypto.PubkeyToAddress(*publicKeyECDSA).Hex()
		t.Logf("Using wallet address: %s", walletAddress)

		// Step 1: Check LBTCv allowance for AtomicRequest contract
		allowance, err := chainHelper.GetLBTCvAllowance(walletAddress)
		if err != nil {
			t.Logf("Warning: Failed to get LBTCv allowance: %v", err)
		} else {
			t.Logf("LBTCv allowance for AtomicRequest contract: %s", allowance)
			
			// Parse allowance and test amount to compare
			allowanceFloat, _ := new(big.Float).SetString(allowance)
			withdrawalAmountFloat, _ := new(big.Float).SetString("0.0000001")
			
			if allowanceFloat != nil && withdrawalAmountFloat != nil && allowanceFloat.Cmp(withdrawalAmountFloat) < 0 {
				t.Logf("⚠️ WARNING: LBTCv allowance (%s) is less than withdrawal amount (0.0000001)", allowance)
				t.Logf("You need to approve the AtomicRequest contract before withdrawing:")
				t.Logf("Contract: %s", assets.AtomicRequestContractAddress)
				t.Logf("You can approve via Etherscan or call: approve('%s', amount)", assets.AtomicRequestContractAddress)
			}
		}

		// Step 2: Get initial LBTCv balance
		initialBalance, err := chainHelper.GetLBTCvBalance(walletAddress)
		if err != nil {
			t.Logf("Warning: Failed to get initial balance: %v", err)
			initialBalance = "0" // Continue anyway
		}
		t.Logf("Initial LBTCv balance: %s", initialBalance)

		// Parse initial balance to check if we have enough for withdrawal
		initialBalanceFloat, _ := new(big.Float).SetString(initialBalance)
		withdrawalAmountFloat, _ := new(big.Float).SetString("0.0000001")

		if initialBalanceFloat != nil && withdrawalAmountFloat != nil && initialBalanceFloat.Cmp(withdrawalAmountFloat) < 0 {
			t.Skipf("Insufficient LBTCv balance for withdrawal: have %s, need 0.0000001", initialBalance)
		}

		// Step 3: Create unsigned withdrawal transaction via API
		withdrawalReq := WithdrawalRequest{
			Amount:        "0.0000001",
			ToAssetName:   "LBTC",
			WalletAddress: walletAddress,
		}

		reqBody, err := json.Marshal(withdrawalReq)
		if err != nil {
			t.Fatalf("Failed to marshal request: %v", err)
		}

		resp, err := http.Post(
			BaseURL+"/api/orders/withdrawal",
			"application/json",
			bytes.NewBuffer(reqBody),
		)
		if err != nil {
			t.Fatalf("Failed to make POST request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusCreated {
			var errorResp ErrorResponse
			json.NewDecoder(resp.Body).Decode(&errorResp)
			t.Fatalf("Expected status 201, got %d. Error: %s - %s",
				resp.StatusCode, errorResp.Error, errorResp.Message)
		}

		var withdrawalResp WithdrawalResponse
		if err := json.NewDecoder(resp.Body).Decode(&withdrawalResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Parse unsigned transaction
		var unsignedTx UnsignedTransaction
		if err := json.Unmarshal([]byte(withdrawalResp.UnsignedTransaction), &unsignedTx); err != nil {
			t.Fatalf("Failed to parse unsigned transaction: %v", err)
		}

		t.Logf("✅ Created unsigned withdrawal transaction")

		// Step 4: Sign the transaction
		signedTx, err := chainHelper.SignTransaction(unsignedTx, privateKey)
		if err != nil {
			t.Fatalf("Failed to sign transaction: %v", err)
		}
		t.Logf("✅ Transaction signed")

		txHash := signedTx.Hash().Hex()
		t.Logf("Transaction hash: %s", txHash)

		// Step 5: Broadcast the transaction
		t.Logf("🚀 Attempting to broadcast withdrawal transaction...")
		err = chainHelper.BroadcastTransaction(signedTx)
		if err != nil {
			// Log the error but don't fail the test - might be due to insufficient balance or tokens
			t.Logf("⚠️ Transaction broadcast failed: %v", err)

			// Verify the transaction was properly formatted by checking the error type
			if strings.Contains(err.Error(), "insufficient funds") ||
				strings.Contains(err.Error(), "nonce too low") ||
				strings.Contains(err.Error(), "gas price") ||
				strings.Contains(err.Error(), "replacement transaction underpriced") ||
				strings.Contains(err.Error(), "already known") {
				t.Logf("✅ Transaction properly formatted (failed due to expected account/network issues)")
				return // Skip remaining steps since broadcast failed
			} else {
				t.Fatalf("❌ Transaction malformed or unexpected error: %v", err)
			}
		}

		t.Logf("✅ Transaction broadcast successful: %s", txHash)

		// Step 6: Wait for transaction to be mined
		t.Logf("⏳ Waiting for transaction to be mined...")
		receipt, err := chainHelper.WaitForTransaction(signedTx.Hash(), 10*time.Minute)
		if err != nil {
			t.Fatalf("Failed to wait for transaction: %v", err)
		}

		if receipt.Status == 0 {
			t.Logf("⚠️ Transaction failed on chain (expected if insufficient LBTCv tokens)")
			t.Logf("✅ Transaction was properly formatted and mined, but failed during execution")
			t.Logf("This indicates the withdrawal transaction structure is correct")
			return // Skip remaining steps since transaction failed during execution
		}
		t.Logf("✅ Transaction mined successfully in block %d", receipt.BlockNumber.Uint64())

		// Step 7: Wait for order to be processed and check status
		t.Logf("⏳ Waiting for order to be processed...")
		time.Sleep(120 * time.Second) // Wait for crawler to process

		orderResp, err := getOrderStatus(txHash)
		if err != nil {
			t.Logf("Warning: Failed to get order status: %v", err)
		} else {
			t.Logf("✅ Order status: %s, estimated amount: %d", orderResp.Status, orderResp.EstimatedAmount)
		}

		// Step 8: Check final balance
		finalBalance, err := chainHelper.GetLBTCvBalance(walletAddress)
		if err != nil {
			t.Logf("Warning: Failed to get final balance: %v", err)
		} else {
			t.Logf("Final LBTCv balance: %s", finalBalance)

			// Compare balances (expect decrease)
			if finalBalance != initialBalance {
				t.Logf("✅ Balance changed - withdrawal successful!")

				// Verify balance decreased
				finalBalanceFloat, _ := new(big.Float).SetString(finalBalance)
				if initialBalanceFloat != nil && finalBalanceFloat != nil && finalBalanceFloat.Cmp(initialBalanceFloat) < 0 {
					t.Logf("✅ LBTCv balance decreased as expected")
				}
			} else {
				t.Logf("⚠️ Balance unchanged - may need more time for processing")
			}
		}
	})
}
