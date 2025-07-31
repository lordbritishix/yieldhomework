package model

import (
	"time"
)

type MonitoredAddress struct {
	ID            int       `db:"id"`
	WalletAddress string    `db:"wallet_address"`
	ChainID       int       `db:"chain_id"`
	CreatedAt     time.Time `db:"created_at"`
}