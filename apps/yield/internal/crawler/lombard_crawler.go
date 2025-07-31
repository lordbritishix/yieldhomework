package crawler

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/ethereum/go-ethereum"
	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethclient"
	"go.uber.org/zap"
	"math/big"
	"strings"
	"sync"
	"time"
	"yield/apps/yield/internal/assets"
	"yield/apps/yield/internal/config"
	"yield/apps/yield/internal/model"
	"yield/apps/yield/internal/repository"
)

// Contract addresses are now managed centrally in assets package

const TellerABI = `[
	{
		"type": "event",
		"name": "Deposit",
		"inputs": [
			{"internalType": "uint256", "name": "nonce", "type": "uint256", "indexed": true},
			{"internalType": "address", "name": "receiver", "type": "address", "indexed": true},
			{"internalType": "address", "name": "depositAsset", "type": "address", "indexed": true},
			{"internalType": "uint256", "name": "depositAmount", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "shareAmount", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "depositTimestamp", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "shareLockPeriodAtTimeOfDeposit", "type": "uint256", "indexed": false}
		]
	}
]`

const AtomicRequestABI = `[
	{
		"type": "event",
		"name": "AtomicRequestUpdated",
		"inputs": [
			{"internalType": "address", "name": "user", "type": "address", "indexed": true},
			{"internalType": "address", "name": "offerToken", "type": "address", "indexed": true},
			{"internalType": "address", "name": "wantToken", "type": "address", "indexed": true},
			{"internalType": "uint256", "name": "amount", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "deadline", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "minPrice", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "timestamp", "type": "uint256", "indexed": false}
		]
	},
	{
		"type": "event",
		"name": "AtomicRequestFulfilled",
		"inputs": [
			{"internalType": "address", "name": "user", "type": "address", "indexed": true},
			{"internalType": "address", "name": "offerToken", "type": "address", "indexed": true},
			{"internalType": "address", "name": "wantToken", "type": "address", "indexed": true},
			{"internalType": "uint256", "name": "offerAmountSpent", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "wantAmountReceived", "type": "uint256", "indexed": false},
			{"internalType": "uint256", "name": "timestamp", "type": "uint256", "indexed": false}
		]
	}
]`

// Event signatures
var (
	DepositEventSig           = crypto.Keccak256Hash([]byte("Deposit(uint256,address,address,uint256,uint256,uint256,uint256)"))
	AtomicRequestUpdatedSig   = crypto.Keccak256Hash([]byte("AtomicRequestUpdated(address,address,address,uint256,uint256,uint256,uint256)"))
	AtomicRequestFulfilledSig = crypto.Keccak256Hash([]byte("AtomicRequestFulfilled(address,address,address,uint256,uint256,uint256)"))
)

type LombardCrawler struct {
	config                     *config.Config
	client                     *ethclient.Client
	db                         *sql.DB
	logger                     *zap.Logger
	tellerAddress              common.Address
	vaultAddress               common.Address
	atomicRequestAddress       common.Address
	supportedTokens            map[common.Address]string // map[address]name
	registeredAddrs            sync.Map                  // map[common.Address]bool
	tellerABI                  abi.ABI
	atomicRequestABI           abi.ABI
	repository                 *repository.CrawlerRepository
	monitoredAddressRepository *repository.MonitoredAddressRepository
}

type CrawlerState struct {
	LastProcessedBlock uint64    `db:"last_processed_block"`
	UpdatedAt          time.Time `db:"updated_at"`
}

// Helper functions for amount conversion and asset name mapping
func (c *LombardCrawler) convertToDecimalAmount(amount *big.Int, decimals int) string {
	// Convert wei to decimal representation
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

func (c *LombardCrawler) getAssetName(assetAddress common.Address) string {
	if name, exists := c.supportedTokens[assetAddress]; exists {
		return name
	}
	return assetAddress.Hex()
}

func NewLombardCrawler(
	config *config.Config,
	db *sql.DB,
	logger *zap.Logger,
	repository *repository.CrawlerRepository,
	monitoredAddressRepository *repository.MonitoredAddressRepository) (*LombardCrawler, error) {
	// Connect to Ethereum client
	client, err := ethclient.Dial(config.RpcURL)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to Ethereum client: %w", err)
	}

	// Parse teller ABI
	parsedTellerABI, err := abi.JSON(strings.NewReader(TellerABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse teller ABI: %w", err)
	}

	// Parse atomic request ABI
	parsedAtomicABI, err := abi.JSON(strings.NewReader(AtomicRequestABI))
	if err != nil {
		return nil, fmt.Errorf("failed to parse atomic request ABI: %w", err)
	}

	// Initialize supported tokens map from asset registry
	supportedTokens := make(map[common.Address]string)
	for _, asset := range assets.GlobalRegistry.GetAllAsArray() {
		supportedTokens[asset.Address] = asset.Symbol
	}

	// Get vault address from asset registry
	lbtcvAsset, exists := assets.GlobalRegistry.GetBySymbol("LBTCv")
	if !exists {
		return nil, fmt.Errorf("LBTCv asset not found in registry")
	}

	crawler := &LombardCrawler{
		client:                     client,
		db:                         db,
		config:                     config,
		logger:                     logger,
		tellerAddress:              common.HexToAddress(assets.TellerContractAddress),
		vaultAddress:               lbtcvAsset.Address,
		atomicRequestAddress:       common.HexToAddress(assets.AtomicRequestContractAddress),
		supportedTokens:            supportedTokens,
		tellerABI:                  parsedTellerABI,
		atomicRequestABI:           parsedAtomicABI,
		repository:                 repository,
		monitoredAddressRepository: monitoredAddressRepository,
	}

	return crawler, nil
}

func (c *LombardCrawler) Start() error {
	c.logger.Info("Starting Lombard BTC Vault crawler...")

	// Start main crawling loop in a goroutine
	errChan := make(chan error, 1)
	go func() {
		errChan <- c.crawlingLoop()
	}()

	// Wait for crawling loop to complete or return error
	return <-errChan
}

func (c *LombardCrawler) crawlingLoop() error {
	ticker := time.NewTicker(12 * time.Second) // Ethereum block time
	defer ticker.Stop()

	lastProcessedBlock, err := c.repository.GetLastProcessedBlock()
	if err != nil {
		return fmt.Errorf("failed to get last processed block: %w", err)
	}

	c.logger.Info("Starting from block", zap.Uint64("block", lastProcessedBlock))

	for range ticker.C {
		latestBlock, err := c.client.BlockNumber(context.Background())
		c.logger.Info("Found latest block", zap.Uint64("block", latestBlock))

		if err != nil {
			c.logger.Error("Error getting latest block", zap.Error(err))
			continue
		}

		// Process blocks with configurable block confirmations
		safeBlock := latestBlock - c.config.FinalityOffset

		if lastProcessedBlock-safeBlock < c.config.FinalityOffset {
			continue
		}

		if safeBlock > lastProcessedBlock {
			if err := c.processBlockRange(lastProcessedBlock+1, safeBlock); err != nil {
				c.logger.Error("Error processing blocks", zap.Uint64("start", lastProcessedBlock+1), zap.Uint64("end", safeBlock), zap.Error(err))
				continue
			}

			// Update local tracking - state is now updated after each chunk in processBlockRange
			lastProcessedBlock = safeBlock
		}
	}

	return nil
}

func (c *LombardCrawler) processBlockRange(fromBlock, toBlock uint64) error {
	// Process in chunks to avoid RPC limits
	chunkSize := c.config.ChunkSize

	for start := fromBlock; start <= toBlock; start += chunkSize {
		end := start + chunkSize - 1
		if end > toBlock {
			end = toBlock
		}

		if end-start < 1 {
			break
		}

		c.logger.Info("Scanning block range for events", zap.Uint64("start", start), zap.Uint64("end", end), zap.Uint64("count", end-start))

		if end > toBlock {
			end = toBlock
		}

		if err := c.processVaultEvents(start, end); err != nil {
			return fmt.Errorf("failed to process chunk %d-%d: %w", start, end, err)
		}

		// Update crawler state after each chunk
		if err := c.repository.UpdateLastProcessedBlock(end); err != nil {
			c.logger.Error("Error updating last processed block after chunk", zap.Uint64("start", start), zap.Uint64("end", end), zap.Error(err))
		}

		// Rate limiting
		time.Sleep(100 * time.Millisecond)
	}

	return nil
}

func (c *LombardCrawler) processVaultEvents(fromBlock, toBlock uint64) error {
	// Filter for Deposit, Exit, and AtomicRequestUpdated events
	query := ethereum.FilterQuery{
		FromBlock: big.NewInt(int64(fromBlock)),
		ToBlock:   big.NewInt(int64(toBlock)),
		Addresses: []common.Address{c.tellerAddress, c.atomicRequestAddress},
		Topics: [][]common.Hash{
			{DepositEventSig, AtomicRequestUpdatedSig, AtomicRequestFulfilledSig}, // OR condition
		},
	}

	logs, err := c.client.FilterLogs(context.Background(), query)
	if err != nil {
		return fmt.Errorf("failed to filter logs: %w", err)
	}

	for _, eventLog := range logs {
		if err := c.processVaultEvent(eventLog); err != nil {
			c.logger.Error("Error processing event", zap.String("tx_hash", eventLog.TxHash.Hex()), zap.Error(err))
			break
		}
	}

	return nil
}

func (c *LombardCrawler) processVaultEvent(eventLog types.Log) error {
	// Get transaction receipt to ensure success
	receipt, err := c.client.TransactionReceipt(context.Background(), eventLog.TxHash)
	if err != nil {
		return fmt.Errorf("failed to get transaction receipt: %w", err)
	}

	if receipt.Status == 0 {
		return nil // Skip failed transactions
	}

	// Get block timestamp
	block, err := c.client.BlockByNumber(context.Background(), big.NewInt(int64(eventLog.BlockNumber)))
	if err != nil {
		return fmt.Errorf("failed to get block: %w", err)
	}

	switch eventLog.Topics[0] {
	case DepositEventSig:
		return c.processDepositEvent(eventLog, time.Unix(int64(block.Time()), 0))
	case AtomicRequestUpdatedSig:
		return c.processAtomicRequestUpdatedEvent(eventLog, time.Unix(int64(block.Time()), 0))
	case AtomicRequestFulfilledSig:
		return c.processAtomicRequestFulfilledEvent(eventLog, time.Unix(int64(block.Time()), 0))
	}

	return nil
}

func (c *LombardCrawler) processDepositEvent(eventLog types.Log, blockTime time.Time) error {
	// Parse Deposit event - non-indexed parameters are in data
	var eventData struct {
		DepositAmount                  *big.Int
		ShareAmount                    *big.Int
		DepositTimestamp               *big.Int
		ShareLockPeriodAtTimeOfDeposit *big.Int
	}

	if err := c.tellerABI.UnpackIntoInterface(&eventData, "Deposit", eventLog.Data); err != nil {
		// Log the error details for debugging
		c.logger.Error("Failed to unpack Deposit event data", zap.String("tx_hash", eventLog.TxHash.Hex()), zap.Error(err), zap.Int("data_length", len(eventLog.Data)), zap.String("raw_data", fmt.Sprintf("%x", eventLog.Data)))

		return err
	}

	// Extract indexed parameters from topics
	// Topics[0] is the event signature hash
	// Topics[1] is nonce (uint256)
	// Topics[2] is receiver (address)
	// Topics[3] is depositAsset (address)
	nonce := eventLog.Topics[1].Big()
	receiver := common.BytesToAddress(eventLog.Topics[2].Bytes())
	depositAsset := common.BytesToAddress(eventLog.Topics[3].Bytes())

	// Only process supported tokens
	if _, isSupported := c.supportedTokens[depositAsset]; !isSupported {
		return nil
	}

	userAddr := receiver // The recipient of vault shares

	// Check if this address is being monitored
	isMonitored, err := c.monitoredAddressRepository.IsAddressMonitored(userAddr.Hex(), 1) // chain_id = 1 for Ethereum mainnet
	if err != nil {
		c.logger.Error("Failed to check if address is monitored", zap.String("address", userAddr.Hex()), zap.Error(err))
		return err
	}

	if !isMonitored {
		return nil // Skip processing this event silently
	}

	// Log found event only for monitored addresses
	c.logger.Info("Found Deposit event", zap.String("address", eventLog.Address.Hex()), zap.String("tx_hash", eventLog.TxHash.Hex()), zap.String("user_address", userAddr.Hex()))

	// Create event blob
	depositEvent := map[string]interface{}{
		"nonce":                                nonce.String(),
		"receiver":                             receiver.Hex(),
		"deposit_asset":                        depositAsset.Hex(),
		"deposit_amount":                       eventData.DepositAmount.String(),
		"share_amount":                         eventData.ShareAmount.String(),
		"deposit_timestamp":                    eventData.DepositTimestamp.String(),
		"share_lock_period_at_time_of_deposit": eventData.ShareLockPeriodAtTimeOfDeposit.String(),
	}

	eventBlob, err := json.Marshal(depositEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Store in outbox
	return c.repository.StoreOutboxEvent(model.OutboxEvent{
		TxHash:        eventLog.TxHash.Hex(),
		EventType:     "deposit",
		Status:        "unsent",
		BlockNumber:   eventLog.BlockNumber,
		LogIndex:      eventLog.Index,
		TxDate:        blockTime,
		Address:       userAddr.Hex(),
		EventBlob:     eventBlob,
		Amount:        c.convertToDecimalAmount(eventData.DepositAmount, 8), // WBTC, LBTC, and cbBTC all use 8 decimals
		FromAssetName: c.getAssetName(depositAsset),
		ToAssetName:   c.getAssetName(c.vaultAddress),
	})
}

func (c *LombardCrawler) processAtomicRequestFulfilledEvent(eventLog types.Log, blockTime time.Time) error {
	// Parse AtomicRequestFulfilled event - non-indexed parameters are in data
	var eventData struct {
		OfferAmountSpent   *big.Int
		WantAmountReceived *big.Int
		Timestamp          *big.Int
	}

	if err := c.atomicRequestABI.UnpackIntoInterface(&eventData, "AtomicRequestFulfilled", eventLog.Data); err != nil {
		// Log the error details for debugging
		c.logger.Error("Failed to unpack AtomicRequestFulfilled event data", zap.String("tx_hash", eventLog.TxHash.Hex()), zap.Error(err), zap.Int("data_length", len(eventLog.Data)), zap.String("raw_data", fmt.Sprintf("%x", eventLog.Data)))
		return err
	}

	// Extract indexed parameters from topics
	// Topics[0] is the event signature hash
	// Topics[1] is user (address)
	// Topics[2] is offerToken (address)
	// Topics[3] is wantToken (address)
	user := common.BytesToAddress(eventLog.Topics[1].Bytes())
	offerToken := common.BytesToAddress(eventLog.Topics[2].Bytes())
	wantToken := common.BytesToAddress(eventLog.Topics[3].Bytes())

	// Check if this address is being monitored
	isMonitored, err := c.monitoredAddressRepository.IsAddressMonitored(user.Hex(), 1) // chain_id = 1 for Ethereum mainnet
	if err != nil {
		c.logger.Error("Failed to check if address is monitored", zap.String("address", user.Hex()), zap.Error(err))
		return err
	}

	if !isMonitored {
		return nil // Skip processing this event silently
	}

	// Log found event only for monitored addresses
	c.logger.Info("Found AtomicRequestFulfilled event", zap.String("address", eventLog.Address.Hex()), zap.String("tx_hash", eventLog.TxHash.Hex()), zap.String("user_address", user.Hex()))

	//// Only record if the offerToken is the Lombard Vault address
	//if offerToken != c.vaultAddress {
	//	return nil
	//}

	// Create event blob
	atomicRequestFulfilledEvent := map[string]interface{}{
		"user":                 user.Hex(),
		"offer_token":          offerToken.Hex(),
		"want_token":           wantToken.Hex(),
		"offer_amount_spent":   eventData.OfferAmountSpent.String(),
		"want_amount_received": eventData.WantAmountReceived.String(),
		"timestamp":            eventData.Timestamp.String(),
	}

	eventBlob, err := json.Marshal(atomicRequestFulfilledEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Use the user address as the wallet address for this event
	userAddr := user.Hex()

	// Store in outbox
	return c.repository.StoreOutboxEvent(model.OutboxEvent{
		TxHash:        eventLog.TxHash.Hex(),
		EventType:     "withdrawal_completed",
		Status:        "unsent",
		BlockNumber:   eventLog.BlockNumber,
		LogIndex:      eventLog.Index,
		TxDate:        blockTime,
		Address:       userAddr,
		EventBlob:     eventBlob,
		Amount:        c.convertToDecimalAmount(eventData.WantAmountReceived, 8), // Use want amount received as the withdrawal amount
		FromAssetName: c.getAssetName(offerToken),
		ToAssetName:   c.getAssetName(wantToken),
	})
}

func (c *LombardCrawler) processAtomicRequestUpdatedEvent(eventLog types.Log, blockTime time.Time) error {
	// Parse AtomicRequestUpdated event - indexed and non-indexed parameters
	// Event signature: AtomicRequestUpdated(address indexed user, address indexed offerToken, address indexed wantToken, uint256 amount, uint256 deadline, uint256 minPrice, uint256 timestamp)
	var eventData struct {
		Amount    *big.Int
		Deadline  *big.Int
		MinPrice  *big.Int
		Timestamp *big.Int
	}

	if err := c.atomicRequestABI.UnpackIntoInterface(&eventData, "AtomicRequestUpdated", eventLog.Data); err != nil {
		// Log the error details for debugging
		c.logger.Error("Failed to unpack AtomicRequestUpdated event data", zap.String("tx_hash", eventLog.TxHash.Hex()), zap.Error(err), zap.Int("data_length", len(eventLog.Data)), zap.String("raw_data", fmt.Sprintf("%x", eventLog.Data)))
		return err
	}

	// Extract indexed parameters from topics
	// Topics[0] is the event signature hash
	// Topics[1] is user (address)
	// Topics[2] is offerToken (address)
	// Topics[3] is wantToken (address)
	user := common.BytesToAddress(eventLog.Topics[1].Bytes())
	offerToken := common.BytesToAddress(eventLog.Topics[2].Bytes())
	wantToken := common.BytesToAddress(eventLog.Topics[3].Bytes())

	// Check if this address is being monitored
	isMonitored, err := c.monitoredAddressRepository.IsAddressMonitored(user.Hex(), 1) // chain_id = 1 for Ethereum mainnet
	if err != nil {
		c.logger.Error("Failed to check if address is monitored", zap.String("address", user.Hex()), zap.Error(err))
		return err
	}

	if !isMonitored {
		return nil // Skip processing this event silently
	}

	// Log found event only for monitored addresses
	c.logger.Info("Found AtomicRequestUpdated event", zap.String("address", eventLog.Address.Hex()), zap.String("tx_hash", eventLog.TxHash.Hex()), zap.String("user_address", user.Hex()))

	// Only record if the offerToken is the Lombard Vault address
	if offerToken != c.vaultAddress {
		return nil
	}

	// Create event blob
	atomicRequestEvent := map[string]interface{}{
		"user":        user.Hex(),
		"offer_token": offerToken.Hex(),
		"want_token":  wantToken.Hex(),
		"amount":      eventData.Amount.String(),
		"deadline":    eventData.Deadline.String(),
		"min_price":   eventData.MinPrice.String(),
		"timestamp":   eventData.Timestamp.String(),
	}

	eventBlob, err := json.Marshal(atomicRequestEvent)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	// Use the user address as the wallet address for this event
	userAddr := user.Hex()

	// Store in outbox
	return c.repository.StoreOutboxEvent(model.OutboxEvent{
		TxHash:        eventLog.TxHash.Hex(),
		EventType:     "withdrawal_requested",
		Status:        "unsent",
		BlockNumber:   eventLog.BlockNumber,
		LogIndex:      eventLog.Index,
		TxDate:        blockTime,
		Address:       userAddr,
		EventBlob:     eventBlob,
		Amount:        c.convertToDecimalAmount(eventData.Amount, 8), // Assuming 8 decimals for consistency
		FromAssetName: c.getAssetName(offerToken),
		ToAssetName:   c.getAssetName(wantToken),
	})
}

func (c *LombardCrawler) Close() error {
	if c.db != nil {
		c.db.Close()
	}
	if c.client != nil {
		c.client.Close()
	}
	return nil
}
