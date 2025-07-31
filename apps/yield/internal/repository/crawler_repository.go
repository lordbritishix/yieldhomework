package repository

import (
	"database/sql"
	"fmt"
	"go.uber.org/zap"
	"yield/apps/yield/internal/model"
)

type CrawlerRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewCrawlerRepository(db *sql.DB, logger *zap.Logger) *CrawlerRepository {
	return &CrawlerRepository{db: db, logger: logger}
}

func (c *CrawlerRepository) GetLastProcessedBlock() (uint64, error) {
	var block uint64
	err := c.db.QueryRow(`
		SELECT last_processed_block FROM crawler_state WHERE id = 1
	`).Scan(&block)
	return block, err
}

func (c *CrawlerRepository) UpdateLastProcessedBlock(block uint64) error {
	_, err := c.db.Exec(`
		UPDATE crawler_state 
		SET last_processed_block = $1, updated_at = NOW() 
		WHERE id = 1
	`, block)
	return err
}

func (c *CrawlerRepository) StoreOutboxEvent(event model.OutboxEvent) error {
	_, err := c.db.Exec(`
		INSERT INTO event_outbox (tx_hash, event_type, status, block_number, log_index, tx_date, wallet_address, event_blob, amount, from_asset_name, to_asset_name)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (tx_hash, log_index) DO UPDATE SET
			status = EXCLUDED.status,
			block_number = EXCLUDED.block_number,
			tx_date = EXCLUDED.tx_date,
			wallet_address = EXCLUDED.wallet_address,
			event_blob = EXCLUDED.event_blob,
			amount = EXCLUDED.amount,
			from_asset_name = EXCLUDED.from_asset_name,
			to_asset_name = EXCLUDED.to_asset_name,
			created_at = NOW()
	`, event.TxHash, event.EventType, event.Status, event.BlockNumber, event.LogIndex, event.TxDate, event.Address, event.EventBlob, event.Amount, event.FromAssetName, event.ToAssetName)

	if err != nil {
		return fmt.Errorf("failed to store outbox event: %w", err)
	}

	c.logger.Info("Stored event", zap.String("event_type", event.EventType), zap.String("address", event.Address), zap.String("tx_hash", event.TxHash))
	return nil
}

func (c *CrawlerRepository) GetUnsentEventsForProcessing(limit int) ([]model.OutboxEvent, error) {
	// Use a transaction to ensure atomicity
	tx, err := c.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() // Will be ignored if tx.Commit() succeeds

	// Select and lock unsent events for processing
	rows, err := tx.Query(`
		SELECT tx_hash, event_type, status, block_number, log_index, tx_date, wallet_address, event_blob, amount, from_asset_name, to_asset_name, created_at
		FROM event_outbox 
		WHERE status = 'unsent' 
		ORDER BY created_at, log_index
		LIMIT $1
		FOR UPDATE SKIP LOCKED
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []model.OutboxEvent
	var eventKeys []struct{ txHash, eventType, logIndex string }

	for rows.Next() {
		var event model.OutboxEvent
		if err := rows.Scan(&event.TxHash, &event.EventType, &event.Status,
			&event.BlockNumber, &event.LogIndex, &event.TxDate, &event.Address, &event.EventBlob, &event.Amount, &event.FromAssetName, &event.ToAssetName, &event.CreatedAt); err != nil {
			return nil, err
		}
		events = append(events, event)
		eventKeys = append(eventKeys, struct{ txHash, eventType, logIndex string }{event.TxHash, event.EventType, fmt.Sprintf("%d", event.LogIndex)})
	}
	rows.Close()

	// Mark selected events as 'processing' to prevent other threads from picking them up
	for _, key := range eventKeys {
		_, err = tx.Exec(`
			UPDATE event_outbox 
			SET status = 'processing' 
			WHERE tx_hash = $1 AND event_type = $2 AND log_index = $3 AND status = 'unsent'
		`, key.txHash, key.eventType, key.logIndex)
		if err != nil {
			return nil, err
		}
	}

	// Commit the transaction
	if err = tx.Commit(); err != nil {
		return nil, err
	}

	return events, nil
}

func (c *CrawlerRepository) MarkEventAsSent(txHash, eventType string, logIndex uint) error {
	_, err := c.db.Exec(`
		UPDATE event_outbox 
		SET status = 'sent'
		WHERE tx_hash = $1 AND event_type = $2 AND log_index = $3
	`, txHash, eventType, logIndex)
	return err
}

func (c *CrawlerRepository) MarkEventAsFailed(txHash, eventType string, logIndex uint) error {
	_, err := c.db.Exec(`
		UPDATE event_outbox 
		SET status = 'unsent'
		WHERE tx_hash = $1 AND event_type = $2 AND log_index = $3 AND status = 'processing'
	`, txHash, eventType, logIndex)
	return err
}
