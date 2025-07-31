package model

import (
	"time"
)

type Order struct {
	OrderID         string     `db:"order_id"`
	TxHash          string     `db:"tx_hash"`
	LogIndex        uint64     `db:"log_index"`
	BlockNumber     uint64     `db:"block_number"`
	TxDate          time.Time  `db:"tx_date"`
	TransferType    string     `db:"transfer_type"` // "deposit" or "withdrawal"
	Status          string     `db:"status"`        // "completed" or "in_progress"
	WalletAddress   string     `db:"wallet_address"`
	Amount          string     `db:"amount"`
	FromAssetName   string     `db:"from_asset_name"`
	ToAssetName     string     `db:"to_asset_name"`
	EstimatedAmount *string    `db:"estimated_amount"` // nullable field
}