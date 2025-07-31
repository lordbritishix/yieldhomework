# Lombard BTC Vault Yield Application

A yield farming application that enables users to deposit and withdraw Bitcoin-based tokens (LBTC, WBTC, cbBTC) into/from Lombard's LBTCv vault on Ethereum mainnet.

## Table of Contents

- [Overview](#overview)
- [Architecture](#architecture)
- [Design Decisions](#design-decisions)
- [Schema Design](#schema-design)
- [API Endpoints](#api-endpoints)
- [Getting Started](#getting-started)
- [Testing](#testing)
- [Prompt for Code Generation](#prompt-for-code-generation)
---

## Overview

This application provides a REST API for yield operations with the following key features:

- **Multi-token Support**: Deposit LBTC, WBTC, or cbBTC tokens to receive LBTCv vault shares
- **Withdrawal Operations**: Unstake LBTCv tokens back to the original asset
- **Balance Checking**: Query token balances across multiple Bitcoin-based assets
- **Vault Information**: Get real-time vault metrics including APY, TVL, and token details
- **Transaction Building**: Generate unsigned Ethereum transactions for user signing
- **Order Tracking**: Monitor deposit/withdrawal status and history
- **Blockchain Integration**: Real-time event crawling and transaction monitoring

---

## Architecture

### High-Level Architecture

```
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│   REST API      │    │   Database      │    │   Blockchain    │
│   (Gorilla Mux) │◄──►│   (PostgreSQL)  │    │   (Ethereum)    │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                        │                       ▲
         ▼                        ▼                       │
┌─────────────────┐    ┌─────────────────┐    ┌─────────────────┐
│  Transaction    │    │   Event         │    │   Event         │
│  Builder        │    │   Publisher     │    │   Crawler       │
└─────────────────┘    └─────────────────┘    └─────────────────┘
         │                        │                       │
         └──────────────┬─────────┴───────────────────────┘
                        ▼
                ┌─────────────────┐
                │     Kafka       │
                │  Message Queue  │
                └─────────────────┘
```

### Component Overview

#### API Layer
- **Server**: HTTP server with middleware for logging and CORS
- **Handlers**: 
  - `OrderHandler`: Manages deposit/withdrawal transactions
  - `BalanceHandler`: Queries token balances from blockchain
  - `InfoHandler`: Provides vault information (APY, TVL, token details)
- **Transaction Builder**: Creates unsigned Ethereum transactions

#### Data Layer
- **Models**: Domain objects (Order, MonitoredAddress, Outbox)
- **Repositories**: Database access layer with proper migrations
- **Database**: PostgreSQL with optimized indexes

#### Blockchain Integration
- **Event Crawler**: Monitors Ethereum events for deposits/withdrawals
- **Event Publisher**: Publishes blockchain events to Kafka
- **Chain Helper**: Abstraction for blockchain operations (testing)

#### Message Queue
- **Kafka**: Event streaming and decoupling of components
- **Event Processing**: Asynchronous handling of blockchain events

---

## Schema Design

### Core Tables

#### `orders`
```sql
CREATE TABLE orders (
    order_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    tx_hash VARCHAR(66) NOT NULL,
    log_index INTEGER NOT NULL,
    block_number BIGINT NOT NULL,
    tx_date TIMESTAMP NOT NULL,
    transfer_type VARCHAR(20) NOT NULL,  -- 'deposit' | 'withdrawal'
    status VARCHAR(20) NOT NULL,         -- 'completed' | 'in_progress'
    wallet_address VARCHAR(42) NOT NULL,
    amount DECIMAL(78,18) NOT NULL, 
    from_asset_name VARCHAR(50) NOT NULL,
    to_asset_name VARCHAR(50) NOT NULL,
    estimated_amount DECIMAL(78,18),
    UNIQUE(tx_hash, log_index)
);
```

#### `monitored_addresses`
```sql
CREATE TABLE monitored_addresses (
    id SERIAL PRIMARY KEY,
    wallet_address VARCHAR(42) NOT NULL,
    chain_id INTEGER NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    UNIQUE(wallet_address, chain_id)
);
```

#### `event_outbox`
```sql
CREATE TABLE event_outbox (
    tx_hash VARCHAR(66) NOT NULL,
    event_type VARCHAR(20) NOT NULL,
    status VARCHAR(20) NOT NULL DEFAULT 'unsent',
    block_number BIGINT NOT NULL,
    log_index INTEGER NOT NULL,
    tx_date TIMESTAMP NOT NULL,
    wallet_address VARCHAR(42) NOT NULL,
    event_blob JSONB NOT NULL,           -- Flexible event data storage
    amount DECIMAL(78,18) NOT NULL,
    from_asset_name VARCHAR(50) NOT NULL,
    to_asset_name VARCHAR(50) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    PRIMARY KEY (tx_hash, log_index)
);
```

#### `crawler_state`
```sql
CREATE TABLE crawler_state (
    id INTEGER PRIMARY KEY DEFAULT 1,
    last_processed_block BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMP DEFAULT NOW(),
    CONSTRAINT single_row CHECK (id = 1)  -- Singleton pattern
);
```
---
## API Endpoints

### Balance Query
```http
GET /api/balance/{wallet_address}

Response:
{
  "wallet_address": "0x...",
  "balances": {
    "LBTC": {
      "balance": "1.25000000",
      "symbol": "LBTC",
      "address": "0x8236a87084f8B84306f72007F36F2618A5634494",
      "decimals": 8
    },
    "WBTC": { ... },
    "CBTC": { ... },
    "LBTCV": { ... }
  }
}
```

### Deposit Transaction
```http
POST /api/orders/deposit
Content-Type: application/json

{
  "amount": "0.001",
  "from_asset_name": "LBTC",
  "wallet_address": "0x..."
}

Response:
{
  "unsigned_transaction": "{...}"  // JSON-encoded transaction
}
```

### Withdrawal Transaction
```http
POST /api/orders/withdrawal
Content-Type: application/json

{
  "amount": "0.001",
  "to_asset_name": "LBTC", 
  "wallet_address": "0x..."
}

Response:
{
  "unsigned_transaction": "{...}"  // JSON-encoded transaction
}
```

### Order Status
```http
GET /api/orders/{tx_hash}

Response:
{
  "order_id": "uuid",
  "tx_hash": "0x...",
  "wallet_address": "0x...",
  "transfer_type": "deposit",
  "status": "completed",
  "amount": "0.001",
  "from_asset_name": "LBTC",
  "to_asset_name": "LBTCV",
  "estimated_amount": "0.00095",
  "tx_date": "2024-01-01T12:00:00Z"
}
```

### Vault Information
```http
GET /api/info

Response:
{
  "apy": "5.25",
  "tvl": "1234.56789",
  "token_symbol": "LBTCv",
  "decimals": 8,
  "vault_name": "Lombard Bitcoin Vault"
}
```

---

## Design Decisions

### 1. **Decimal Precision Handling**
- **Problem**: Bitcoin tokens use 8 decimals, Ethereum uses 18 decimals
- **Solution**: Separate conversion functions (`convertTo8Decimals`) and proper `DECIMAL(78,18)` storage
- **Rationale**: Prevents precision loss and maintains accuracy for financial calculations

### 2. **Unsigned Transaction Pattern**
- **Problem**: Users need to sign transactions from their own wallets
- **Solution**: API returns unsigned transactions with all parameters pre-calculated
- **Rationale**: Security (no private key handling) and flexibility (supports any wallet)

### 3. **Event-Driven Architecture**
- **Problem**: Need to track blockchain events asynchronously
- **Solution**: Kafka-based event streaming with outbox pattern. Ordered event consumption by wallet address
- **Rationale**: Scalability, reliability, and decoupling of components

### 4. **Singleton Crawler State**
- **Problem**: Multiple crawler instances could process same blocks
- **Solution**: Single row table with CHECK constraint
- **Rationale**: Ensures exactly-once processing and data consistency

### 5. **Environment-Based Configuration**
- **Problem**: Different settings for development, testing, and production
- **Solution**: `.env` file support with fallback defaults
- **Rationale**: Security and deployment flexibility

### 6. **Crawler Efficiency**
- **Problem**: There are many events in the ethereum blockchain
- **Solution**: Only monitored addresses are parsed for the blockchain lombard btc vault deposit and withdrawal events. Addresses are added using the POST /api/orders endpoint. Also, use the eth_getLogs endpoint to better traverse events.
- **Rationale**: Better efficiency for the crawler

---

## Getting Started

### Prerequisites
- Go 1.23+
- Docker & Docker Compose
- Ethereum RPC URL (Alchemy/Infura)

### Environment Setup
```bash
Edit .env with your RPC URL

# Start infrastructure
docker-compose up -d postgres kafka zookeeper

# Build and run the application
go build -o bin/yield ./apps/yield/cmd
./bin/yield
```

### Configuration
```env
# Blockchain
ETHEREUM_RPC_URL=https://eth-mainnet.g.alchemy.com/v2/YOUR_API_KEY

# Database
DB_URL=postgres://user:12345678@localhost:5433/yield?sslmode=disable

# Kafka
KAFKA_BROKER=localhost:9092
KAFKA_TOPIC=lombard-vault-events

# Testing (optional) on test/.env file
TEST_PRIVATE_KEY=your_private_key_for_testing
```

---

## Testing

### Running Tests
```bash
# Run all tests
go test -v ./apps/yield/test

# Run specific test suites
go test -v ./apps/yield/test -run TestCreateDepositTransaction
go test -v ./apps/yield/test -run TestCreateWithdrawalTransaction
go test -v ./apps/yield/test -run TestGetWalletBalance
go test -v ./apps/yield/test -run TestGetVaultInfo

# Run mainnet integration tests (requires private key)
# Note that when running the mainnet tests:
## Spending limit approval needs to be executed first against LBTC and LBTCv tokens before a successful deposit or withdrawal occurs
## The test will stake 0.0000001 LBTC
## The test will unstake 0.0001 LBTCv (The minimum amount that can be unstaked is 0.00010001)
# These 2 tests will use real money and will require gas. Make sure the private key is set on .env file in the test folder
go test -v ./apps/yield/test -run TestCreateDepositTransactionMainnet
go test -v ./apps/yield/test -run TestCreateWithdrawalTransactionMainnet

```

### Test Coverage
```
├── Basic Integration Tests
│   ├── API endpoint validation
│   ├── Request/response structure verification
│   ├── Error handling scenarios
│   └── Business logic validation
├── Mainnet Integration Tests  
│   ├── Transaction signing and broadcasting
│   ├── Balance checking and allowance verification
│   ├── Order status tracking
│   └── End-to-end deposit/withdrawal flows
└── Unit Tests
    ├── Transaction builder validation
    ├── ABI encoding verification
    └── Helper function testing
```
---

## Prompt for Code Generation

```
Please create a yield farming application with the following requirements:

1. **Balance Endpoint (GET /api/balance/{wallet_address})**:
   - Return balances for LBTC, WBTC, cbBTC, and LBTCv tokens
   - Use ERC20 balanceOf calls to Ethereum mainnet
   - Handle 8-decimal precision for Bitcoin tokens
   - Return structured JSON with token details

2. **Deposit Endpoint (POST /api/orders/deposit)**:
   - Accept: amount, from_asset_name (LBTC/WBTC/CBTC), wallet_address
   - Call TellerWithMultiAssetSupport.deposit() method
   - Target contract: 0x4e8f5128f473c6948127f9cbca474a6700f99bab
   - Return unsigned transaction for user signing
   - Use proper 8-decimal conversion for BTC tokens

3. **Withdrawal Endpoint (POST /api/orders/withdrawal)**:
   - Accept: amount, to_asset_name (LBTC/WBTC/CBTC), wallet_address  
   - Call AtomicRequest.safeUpdateAtomicRequest() method
   - Target contract: 0x3b4aCd8879fb60586cCd74bC2F831A4C5E7DbBf8
   - Use tuple parameter structure for userRequest
   - Set 3-day deadline, 100 discount, inSolve=false
   - Return unsigned transaction for user signing

4. **Order Status Endpoint (GET /api/orders/{tx_hash})**:
   - Return order details including status and estimated amounts
   - Support both deposit and withdrawal order types

5. **Infrastructure**:
   - Go application with Gorilla Mux router
   - PostgreSQL database with proper schema
   - Kafka integration for event processing
   - Docker Compose setup
   - Structured logging with Zap
   - Comprehensive error handling

6. **Testing**:
   - Integration tests for all endpoints
   - Mainnet integration tests with real blockchain interaction
   - ChainHelper struct for blockchain operations
   - Environment-based configuration (.env support)
   - Balance verification and transaction broadcasting tests

7. **Blockchain Integration**:
   - Event crawler for monitoring deposits/withdrawals
   - Transaction builder with proper ABI encoding
   - Support for Ethereum mainnet via RPC
   - Proper nonce and gas price management

Technical Requirements:
- Use go 
- Use postgres for the db
- Use kafka for the event stream message bus
- Consider block finality on the ethereum mainnet when crawling the next block. Use 30 blocks.
- Use outbox pattern for on-chain events to make event processing durable. Use wallet address as partition for sequential event processing per wallet address
- Use proper decimal handling (8 decimals for BTC tokens vs 18 for ETH)
- Implement raw transaction creation only on deposit or withdrawals. Users will sign their own transactions
- Create modular, testable code architecture
- Follow Go best practices and error handling
- Support for both development and production environments

Contract Addresses:
- LBTC: 0x8236a87084f8B84306f72007F36F2618A5634494
- WBTC: 0x2260FAC5E5542a773Aa44fBCfeDf7C193bc2C599
- cbBTC: 0xcbB7C0000aB88B473b1f5aFd9ef808440eed33Bf
- LBTCv: 0x5401b8620E5FB570064CA9114fd1e135fd77D57c
- Teller: 0x4e8f5128f473c6948127f9cbca474a6700f99bab
- AtomicRequest: 0x3b4aCd8879fb60586cCd74bC2F831A4C5E7DbBf8
- Accountant: 0x28634D0c5edC67CF2450E74deA49B90a4FF93dCE

Implementation Details:

The crawler is responsible for:
1. Monitoring LBTC or WBTC deposits or withdrawals from the Lombard BTC Vault from the ethereum chain for registered addresses. Deposits into the lombard vault use the event called Deposit and Withdrawals from the lombard vault uses the event called AtomicRequestUpdated (the withdrawal is requested) and AtomicRequestFulfilled (the withdrawal is completed).
2. Crawled deposit or withdrawal events are stored in postgres which will be used as an event outbox. The outbox will contain the following fields:
    1. Event type (enter, exit) - enter if the token is going to the lombard vault and exit if the token is going out of the vault
    2. A status field that initially is set to unsent. Other possible value is sent
    3. Transaction Hash
    4. Block Number
    5. Transaction Date
    6. Wallet address
    7. Event blob as jsonb
    8. Amount, converted into the approproate unit
    9. Asset name if known, or use the asset id
3. Save last parsed block of the crawler so it does not have to parse again from the beginning
4. Consider block finality when attepmpting to process the latest vault. We can use 30 blocks but make it configurable
5. The lombard smart contract is found in 0x5401b8620E5FB570064CA9114fd1e135fd77D57c
6. Use alchemy as a provider for the eth client. Use a reasonable rate limitting mechanism.
7. When scanning the events in the blockchain, use the eth_getLogs function as it can scan many blocks at once for events that we are interested in
8. Note that you can only do one withdrawal at a time. If there is a withdrawal that is already active, you cannot issue a widthrawal request anymore. We can use this property to keep track of whether a withdrawal is still pending or complete for a given address.
9. When there is an error in parsing or processing the event, I want it to not fail silently so it can be retried again

The event publisher is responsible for:
1. Scanning events in the outbox table for unsent events in a cron like manner.
2. Publishing the unsent events into kafka. Use the fields:
    * Event type
    * Tx Hash
    * Block number
    * Tx Date
    * Event data
    * Wallet Address
    * Amount
    * From Asset Name
    * To Asset Name
    * Timestamp
3. Use lombard-vault-events for the kafka topic. Use the wallet address as the event key so that events for the same wallet land in the same partition. 
4. Once published, mark the event as sent.
5. Make sure the event publiusher is thread safe and only grabs records not grabbed by other threads. Maybe use SELECT FOR UPDATE SKIP LOCKED.

The order materializer is responsible for listening to the lombard-vault-events and materializing the events into the order table in postgres. Put this in another package under internal/transfer_materializer
1. The order table contains the following fields
    * Order Id (primary key)
    * Tx hash (compound unique key)
    * Block number (compound unique key)
    * Tx Date
    * Transfer type (deposit if the event type is deposit, withdrawal if the event type is withdrawal_requested or withdrawal_completed)
    * Status
        1. If the event type is withdrawal_completed, search for the last withdrawal for that given wallet on the orders table that has a status of in_progress, and mark that as completed. This works because protocol dictates that only one withdrawal request can be active at a time. Make sure that the fields are properly indexed for this lookup to be efficient.
        2. If the event type is withdrawal_requested, status is in_progress.
        3. If the event type is deposit, status is completed.
    * Wallet address
    * Amount
    * Estimated Amount
    * Asset Name
2. For the calculation of the estimated amount: 
    * For the event whose type is withdrawal_requested, set this value by parsing the event_blob and dividing the amount (unit adjusted) by min_price (unit adjusted)
    * For the event whose type is withdrawal_completed, if the value is already populated, don’t set the value. Otherwise, use the amount in this field.
    * For the evnet type whose type is deposit, do not put anything in the estimated amount.
```
---
