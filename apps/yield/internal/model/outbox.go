package model

import (
	"encoding/json"
	"time"
)

type OutboxEvent struct {
	TxHash        string          `db:"tx_hash"`
	EventType     string          `db:"event_type"`
	Status        string          `db:"status"`
	BlockNumber   uint64          `db:"block_number"`
	LogIndex      uint            `db:"log_index"`
	TxDate        time.Time       `db:"tx_date"`
	Address       string          `db:"wallet_address"`
	EventBlob     json.RawMessage `db:"event_blob"`
	Amount        string          `db:"amount"`
	FromAssetName string          `db:"from_asset_name"`
	ToAssetName   string          `db:"to_asset_name"`
	CreatedAt     time.Time       `db:"created_at"`
}
