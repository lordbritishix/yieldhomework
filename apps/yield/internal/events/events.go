package events

import (
	"encoding/json"
	"time"
)

type TransferEvent struct {
	EventType     string          `json:"event_type"`
	TxHash        string          `json:"tx_hash"`
	BlockNumber   uint64          `json:"block_number"`
	LogIndex      uint64          `json:"log_index"`
	TxDate        time.Time       `json:"tx_date"`
	WalletAddress string          `json:"wallet_address"`
	EventData     json.RawMessage `json:"event_data"`
	Amount        string          `json:"amount"`
	FromAssetName string          `json:"from_asset_name"`
	ToAssetName   string          `json:"to_asset_name"`
	Timestamp     time.Time       `json:"timestamp"`
}
