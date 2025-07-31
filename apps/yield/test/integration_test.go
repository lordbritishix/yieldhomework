package test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"yield/apps/yield/internal/assets"
)

// All shared constants and types are now defined in common.go

func TestCreateDepositTransaction(t *testing.T) {
	// Test: Create a deposit transaction (no order created)
	t.Run("CreateDepositTransaction", func(t *testing.T) {
		depositReq := DepositRequest{
			Amount:        TestAmount,
			FromAssetName: TestFromAsset,
			WalletAddress: TestWalletAddress,
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

		// Validate response
		if depositResp.UnsignedTransaction == "" {
			t.Error("UnsignedTransaction should not be empty")
		}

		// Parse and validate unsigned transaction
		var unsignedTx UnsignedTransaction
		if err := json.Unmarshal([]byte(depositResp.UnsignedTransaction), &unsignedTx); err != nil {
			t.Fatalf("Failed to parse unsigned transaction: %v", err)
		}

		// Validate transaction fields
		if unsignedTx.To == "" {
			t.Error("Transaction 'to' field should not be empty")
		}

		if unsignedTx.Data == "" {
			t.Error("Transaction 'data' field should not be empty")
		}

		if unsignedTx.Value != "0x0" {
			t.Errorf("Expected value to be '0x0', got '%s'", unsignedTx.Value)
		}

		if unsignedTx.GasLimit == "" {
			t.Error("Transaction 'gas_limit' field should not be empty")
		}

		if unsignedTx.GasPrice == "" {
			t.Error("Transaction 'gas_price' field should not be empty")
		}

		if unsignedTx.ChainID != "1" {
			t.Errorf("Expected chain_id to be '1', got '%s'", unsignedTx.ChainID)
		}

		if unsignedTx.Nonce == "" {
			t.Error("Transaction 'nonce' field should not be empty")
		}

		t.Logf("✅ Created unsigned transaction: %s", depositResp.UnsignedTransaction)
	})
}

func TestGetOrderByTxHash(t *testing.T) {
	// Test: Get order by transaction hash (requires existing order in database)
	t.Run("GetOrderByTxHash", func(t *testing.T) {
		// Use a known transaction hash from the database
		// For this test, we'll use a placeholder - in real testing you'd have actual tx hashes
		testTxHash := "0x1234567890abcdef1234567890abcdef1234567890abcdef1234567890abcdef"
		getURL := fmt.Sprintf("%s/api/orders/%s", BaseURL, testTxHash)

		resp, err := http.Get(getURL)
		if err != nil {
			t.Fatalf("Failed to make GET request: %v", err)
		}
		defer resp.Body.Close()

		// This should return 404 since the transaction hash doesn't exist
		if resp.StatusCode != http.StatusNotFound {
			t.Errorf("Expected status 404 for non-existent tx hash, got %d", resp.StatusCode)
		}

		var errorResp ErrorResponse
		if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
			t.Fatalf("Failed to decode error response: %v", err)
		}

		if errorResp.Error != "order_not_found" {
			t.Errorf("Expected error 'order_not_found', got '%s'", errorResp.Error)
		}

		t.Logf("✅ Non-existent tx hash correctly returned 404 with error: %s", errorResp.Error)
	})
}

func TestCreateDepositTransactionValidation(t *testing.T) {
	tests := []struct {
		name           string
		request        DepositRequest
		expectedStatus int
		expectedError  string
	}{
		{
			name: "MissingAmount",
			request: DepositRequest{
				FromAssetName: TestFromAsset,
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_amount",
		},
		{
			name: "MissingFromAssetName",
			request: DepositRequest{
				Amount:        TestAmount,
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_from_asset_name",
		},
		{
			name: "MissingWalletAddress",
			request: DepositRequest{
				Amount:        TestAmount,
				FromAssetName: TestFromAsset,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_wallet_address",
		},
		{
			name: "UnsupportedAsset",
			request: DepositRequest{
				Amount:        TestAmount,
				FromAssetName: "INVALID",
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "unsupported_asset",
		},
		{
			name: "CaseInsensitiveAsset",
			request: DepositRequest{
				Amount:        TestAmount,
				FromAssetName: "lbtc", // lowercase
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusCreated,
			expectedError:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reqBody, err := json.Marshal(test.request)
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

			if resp.StatusCode != test.expectedStatus {
				t.Errorf("Expected status %d, got %d", test.expectedStatus, resp.StatusCode)
			}

			if test.expectedError != "" {
				var errorResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
					t.Fatalf("Failed to decode error response: %v", err)
				}

				if errorResp.Error != test.expectedError {
					t.Errorf("Expected error '%s', got '%s'", test.expectedError, errorResp.Error)
				}

				t.Logf("✅ Validation test '%s' returned expected error: %s", test.name, errorResp.Error)
			} else {
				// For successful cases, validate we get the expected response
				var depositResp DepositResponse
				if err := json.NewDecoder(resp.Body).Decode(&depositResp); err != nil {
					t.Fatalf("Failed to decode success response: %v", err)
				}

				if depositResp.UnsignedTransaction == "" {
					t.Error("UnsignedTransaction should not be empty for successful request")
				}

				t.Logf("✅ Validation test '%s' succeeded", test.name)
			}
		})
	}
}

func TestGetNonExistentOrder(t *testing.T) {
	nonExistentTxHash := "0x0000000000000000000000000000000000000000000000000000000000000000" // Non-existent tx hash
	getURL := fmt.Sprintf("%s/api/orders/%s", BaseURL, nonExistentTxHash)

	resp, err := http.Get(getURL)
	if err != nil {
		t.Fatalf("Failed to make GET request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}

	var errorResp ErrorResponse
	if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	if errorResp.Error != "order_not_found" {
		t.Errorf("Expected error 'order_not_found', got '%s'", errorResp.Error)
	}

	t.Logf("✅ Non-existent order correctly returned 404 with error: %s", errorResp.Error)
}

func TestHealthCheck(t *testing.T) {
	resp, err := http.Get(BaseURL + "/api/health")
	if err != nil {
		t.Fatalf("Failed to make GET request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var healthResp map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&healthResp); err != nil {
		t.Fatalf("Failed to decode health response: %v", err)
	}

	if healthResp["status"] != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", healthResp["status"])
	}

	t.Logf("✅ Health check passed: %v", healthResp)
}

func TestGetWalletBalance(t *testing.T) {
	// Test: Get wallet balance for LBTC, WBTC, cbBTC, and LBTCv tokens using real blockchain data
	t.Run("GetWalletBalance", func(t *testing.T) {
		getURL := fmt.Sprintf("%s/api/balance/%s", BaseURL, TestWalletAddress)

		resp, err := http.Get(getURL)
		if err != nil {
			t.Fatalf("Failed to make GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errorResp ErrorResponse
			json.NewDecoder(resp.Body).Decode(&errorResp)
			t.Fatalf("Expected status 200, got %d. Error: %s - %s",
				resp.StatusCode, errorResp.Error, errorResp.Message)
		}

		var balanceResp BalanceResponse
		if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Validate response structure
		if balanceResp.WalletAddress != TestWalletAddress {
			t.Errorf("Expected wallet address %s, got %s", TestWalletAddress, balanceResp.WalletAddress)
		}

		// Expected tokens
		expectedTokens := []string{"LBTC", "WBTC", "CBTC", "LBTCv"}

		if len(balanceResp.Balances) != len(expectedTokens) {
			t.Errorf("Expected %d tokens, got %d", len(expectedTokens), len(balanceResp.Balances))
		}

		// Validate each expected token is present
		for _, expectedToken := range expectedTokens {
			balance, exists := balanceResp.Balances[expectedToken]
			if !exists {
				t.Errorf("Expected token %s not found in response", expectedToken)
				continue
			}

			// Validate token structure
			if balance.Symbol == "" {
				t.Errorf("Token %s has empty symbol", expectedToken)
			}

			if balance.Address == "" {
				t.Errorf("Token %s has empty address", expectedToken)
			}

			if balance.Decimals != 8 {
				t.Errorf("Token %s expected 8 decimals, got %d", expectedToken, balance.Decimals)
			}

			if balance.Balance == "" {
				t.Errorf("Token %s has empty balance", expectedToken)
			}

			t.Logf("✅ Token %s: Balance=%s, Address=%s, Decimals=%d",
				balance.Symbol, balance.Balance, balance.Address, balance.Decimals)
		}

		t.Logf("✅ Successfully retrieved balances for wallet %s", TestWalletAddress)
	})
}

func TestGetWalletBalanceValidation(t *testing.T) {
	tests := []struct {
		name           string
		walletAddress  string
		expectedStatus int
		expectedError  string
	}{
		{
			name:           "MissingWalletAddress",
			walletAddress:  "",
			expectedStatus: http.StatusNotFound, // Gorilla mux returns 404 for missing path param
		},
		{
			name:           "InvalidWalletAddress",
			walletAddress:  "invalid-address",
			expectedStatus: http.StatusBadRequest,
			expectedError:  "invalid_wallet_address",
		},
		{
			name:           "ValidWalletAddress",
			walletAddress:  TestWalletAddress,
			expectedStatus: http.StatusOK,
		},
		{
			name:           "AnotherValidAddress",
			walletAddress:  "0x742d35Cc52C0b9550e0B7e5c5B8cd5D9E3e5C5c5", // Another valid format
			expectedStatus: http.StatusOK,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var getURL string
			if test.walletAddress == "" {
				getURL = BaseURL + "/api/balance/"
			} else {
				getURL = fmt.Sprintf("%s/api/balance/%s", BaseURL, test.walletAddress)
			}

			resp, err := http.Get(getURL)
			if err != nil {
				t.Fatalf("Failed to make GET request: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != test.expectedStatus {
				t.Errorf("Expected status %d, got %d", test.expectedStatus, resp.StatusCode)
			}

			if test.expectedError != "" {
				var errorResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
					t.Fatalf("Failed to decode error response: %v", err)
				}

				if errorResp.Error != test.expectedError {
					t.Errorf("Expected error '%s', got '%s'", test.expectedError, errorResp.Error)
				}

				t.Logf("✅ Validation test '%s' returned expected error: %s", test.name, errorResp.Error)
			} else if test.expectedStatus == http.StatusOK {
				// For successful cases, validate we get the expected response structure
				var balanceResp BalanceResponse
				if err := json.NewDecoder(resp.Body).Decode(&balanceResp); err != nil {
					t.Fatalf("Failed to decode success response: %v", err)
				}

				if len(balanceResp.Balances) == 0 {
					t.Error("Expected non-empty balances for successful request")
				}

				t.Logf("✅ Validation test '%s' succeeded with %d tokens", test.name, len(balanceResp.Balances))
			}
		})
	}
}

func TestCreateWithdrawalTransaction(t *testing.T) {
	// Test: Create a withdrawal transaction (LBTCv to target asset)
	t.Run("CreateWithdrawalTransaction", func(t *testing.T) {
		withdrawalReq := WithdrawalRequest{
			Amount:        TestWithdrawalAmount,
			ToAssetName:   TestToAsset,
			WalletAddress: TestWalletAddress,
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

		// Validate response
		if withdrawalResp.UnsignedTransaction == "" {
			t.Error("UnsignedTransaction should not be empty")
		}

		// Parse and validate unsigned transaction
		var unsignedTx UnsignedTransaction
		if err := json.Unmarshal([]byte(withdrawalResp.UnsignedTransaction), &unsignedTx); err != nil {
			t.Fatalf("Failed to parse unsigned transaction: %v", err)
		}

		// Validate transaction fields
		if unsignedTx.To == "" {
			t.Error("Transaction 'to' field should not be empty")
		}

		// Should target the AtomicRequest contract
		expectedTo := assets.AtomicRequestContractAddress
		if !strings.EqualFold(unsignedTx.To, expectedTo) {
			t.Errorf("Expected 'to' address to be '%s', got '%s'", expectedTo, unsignedTx.To)
		}

		if unsignedTx.Data == "" {
			t.Error("Transaction 'data' field should not be empty")
		}

		if unsignedTx.Value != "0x0" {
			t.Errorf("Expected value to be '0x0', got '%s'", unsignedTx.Value)
		}

		if unsignedTx.GasLimit == "" {
			t.Error("Transaction 'gas_limit' field should not be empty")
		}

		if unsignedTx.GasPrice == "" {
			t.Error("Transaction 'gas_price' field should not be empty")
		}

		if unsignedTx.ChainID != "1" {
			t.Errorf("Expected chain_id to be '1', got '%s'", unsignedTx.ChainID)
		}

		if unsignedTx.Nonce == "" {
			t.Error("Transaction 'nonce' field should not be empty")
		}

		t.Logf("✅ Created unsigned withdrawal transaction: %s", withdrawalResp.UnsignedTransaction)
	})
}

func TestCreateWithdrawalTransactionValidation(t *testing.T) {
	tests := []struct {
		name           string
		request        WithdrawalRequest
		expectedStatus int
		expectedError  string
	}{
		{
			name: "MissingAmount",
			request: WithdrawalRequest{
				ToAssetName:   TestToAsset,
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_amount",
		},
		{
			name: "MissingToAssetName",
			request: WithdrawalRequest{
				Amount:        TestWithdrawalAmount,
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_to_asset_name",
		},
		{
			name: "MissingWalletAddress",
			request: WithdrawalRequest{
				Amount:      TestWithdrawalAmount,
				ToAssetName: TestToAsset,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "missing_wallet_address",
		},
		{
			name: "UnsupportedAsset",
			request: WithdrawalRequest{
				Amount:        TestWithdrawalAmount,
				ToAssetName:   "INVALID",
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusBadRequest,
			expectedError:  "unsupported_asset",
		},
		{
			name: "CaseInsensitiveAsset",
			request: WithdrawalRequest{
				Amount:        TestWithdrawalAmount,
				ToAssetName:   "wbtc", // lowercase
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusCreated,
			expectedError:  "",
		},
		{
			name: "CBTCAsset",
			request: WithdrawalRequest{
				Amount:        TestWithdrawalAmount,
				ToAssetName:   "CBTC",
				WalletAddress: TestWalletAddress,
			},
			expectedStatus: http.StatusCreated,
			expectedError:  "",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			reqBody, err := json.Marshal(test.request)
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

			if resp.StatusCode != test.expectedStatus {
				t.Errorf("Expected status %d, got %d", test.expectedStatus, resp.StatusCode)
			}

			if test.expectedError != "" {
				var errorResp ErrorResponse
				if err := json.NewDecoder(resp.Body).Decode(&errorResp); err != nil {
					t.Fatalf("Failed to decode error response: %v", err)
				}

				if errorResp.Error != test.expectedError {
					t.Errorf("Expected error '%s', got '%s'", test.expectedError, errorResp.Error)
				}

				t.Logf("✅ Validation test '%s' returned expected error: %s", test.name, errorResp.Error)
			} else {
				// For successful cases, validate we get the expected response
				var withdrawalResp WithdrawalResponse
				if err := json.NewDecoder(resp.Body).Decode(&withdrawalResp); err != nil {
					t.Fatalf("Failed to decode success response: %v", err)
				}

				if withdrawalResp.UnsignedTransaction == "" {
					t.Error("UnsignedTransaction should not be empty for successful request")
				}

				t.Logf("✅ Validation test '%s' succeeded", test.name)
			}
		})
	}
}

func TestGetVaultInfo(t *testing.T) {
	// Test: Get vault information including APY, TVL, token symbol/decimals, and vault name
	t.Run("GetVaultInfo", func(t *testing.T) {
		resp, err := http.Get(BaseURL + "/api/info")
		if err != nil {
			t.Fatalf("Failed to make GET request: %v", err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			var errorResp ErrorResponse
			json.NewDecoder(resp.Body).Decode(&errorResp)
			t.Fatalf("Expected status 200, got %d. Error: %s - %s",
				resp.StatusCode, errorResp.Error, errorResp.Message)
		}

		var infoResp InfoResponse
		if err := json.NewDecoder(resp.Body).Decode(&infoResp); err != nil {
			t.Fatalf("Failed to decode response: %v", err)
		}

		// Validate response structure
		if infoResp.APY == "" {
			t.Error("APY should not be empty")
		}

		if infoResp.TVL == "" {
			t.Error("TVL should not be empty")
		}

		if infoResp.TokenSymbol == "" {
			t.Error("TokenSymbol should not be empty")
		}

		if infoResp.Decimals <= 0 {
			t.Errorf("Decimals should be positive, got %d", infoResp.Decimals)
		}

		if infoResp.VaultName == "" {
			t.Error("VaultName should not be empty")
		}

		// Validate that APY is a valid number (should be parseable as float)
		_, err = strconv.ParseFloat(infoResp.APY, 64)
		if err != nil {
			t.Errorf("APY should be a valid number, got '%s': %v", infoResp.APY, err)
		}

		// Validate that TVL is a valid number (should be parseable as float)
		_, err = strconv.ParseFloat(infoResp.TVL, 64)
		if err != nil {
			t.Errorf("TVL should be a valid number, got '%s': %v", infoResp.TVL, err)
		}

		// Log the vault information
		t.Logf("✅ Vault Info: APY=%s%%, TVL=%s, Symbol=%s, Decimals=%d, Name=%s",
			infoResp.APY, infoResp.TVL, infoResp.TokenSymbol, infoResp.Decimals, infoResp.VaultName)

		// Validate expected values for Lombard BTC vault
		if infoResp.TokenSymbol != "LBTCv" {
			t.Errorf("Expected token symbol to be 'LBTCv', got '%s'", infoResp.TokenSymbol)
		}

		if infoResp.Decimals != 8 {
			t.Errorf("Expected decimals to be 8 for BTC-based asset, got %d", infoResp.Decimals)
		}

		// The vault name should contain "Lombard" or "LBTC" (case insensitive)
		vaultNameLower := strings.ToLower(infoResp.VaultName)
		if !strings.Contains(vaultNameLower, "lombard") && !strings.Contains(vaultNameLower, "lbtc") {
			t.Errorf("Expected vault name to contain 'Lombard' or 'LBTC', got '%s'", infoResp.VaultName)
		}

		t.Logf("✅ Successfully retrieved vault information")
	})
}
