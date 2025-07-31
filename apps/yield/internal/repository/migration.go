package repository

import (
	"database/sql"
	"fmt"
)

// InitMigration initializes the database. In production, this would use a proper migration
// library like go-migrate
func InitMigration(db *sql.DB) error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS event_outbox (
			tx_hash VARCHAR(66) NOT NULL,
			event_type VARCHAR(20) NOT NULL,
			status VARCHAR(20) NOT NULL DEFAULT 'unsent',
			block_number BIGINT NOT NULL,
			log_index INTEGER NOT NULL,
			tx_date TIMESTAMP NOT NULL,
			wallet_address VARCHAR(42) NOT NULL,
			event_blob JSONB NOT NULL,
			amount DECIMAL(78,18) NOT NULL,
			from_asset_name VARCHAR(50) NOT NULL,
			to_asset_name VARCHAR(50) NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			PRIMARY KEY (tx_hash, log_index)
		)`,
		`CREATE TABLE IF NOT EXISTS orders (
			order_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
			tx_hash VARCHAR(66) NOT NULL,
			log_index INTEGER NOT NULL,
			block_number BIGINT NOT NULL,
			tx_date TIMESTAMP NOT NULL,
			transfer_type VARCHAR(20) NOT NULL,
			status VARCHAR(20) NOT NULL,
			wallet_address VARCHAR(42) NOT NULL,
			amount DECIMAL(78,18) NOT NULL,
			from_asset_name VARCHAR(50) NOT NULL,
			to_asset_name VARCHAR(50) NOT NULL,
			estimated_amount DECIMAL(78,18),
			UNIQUE(tx_hash, log_index)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_orders_wallet_type_status_date ON orders (wallet_address, transfer_type, status, tx_date DESC)`,
		`CREATE TABLE IF NOT EXISTS monitored_addresses (
			id SERIAL PRIMARY KEY,
			wallet_address VARCHAR(42) NOT NULL,
			chain_id INTEGER NOT NULL,
			created_at TIMESTAMP NOT NULL DEFAULT NOW(),
			UNIQUE(wallet_address, chain_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_monitored_addresses_wallet ON monitored_addresses (wallet_address)`,
		`CREATE TABLE IF NOT EXISTS crawler_state (
			id INTEGER PRIMARY KEY DEFAULT 1,
			last_processed_block BIGINT NOT NULL DEFAULT 22800181,
			updated_at TIMESTAMP DEFAULT NOW(),
			CONSTRAINT single_row CHECK (id = 1)
		)`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return fmt.Errorf("failed to execute query %s: %w", query, err)
		}
	}

	// Initialize crawler state if not exists
	_, err := db.Exec(`
		INSERT INTO crawler_state (id, last_processed_block) 
		VALUES (1, 0) 
		ON CONFLICT (id) DO NOTHING
	`)

	return err
}
