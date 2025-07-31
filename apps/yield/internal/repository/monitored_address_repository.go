package repository

import (
	"database/sql"
	"fmt"
	"go.uber.org/zap"
	"yield/apps/yield/internal/model"
)

type MonitoredAddressRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewMonitoredAddressRepository(db *sql.DB, logger *zap.Logger) *MonitoredAddressRepository {
	return &MonitoredAddressRepository{db: db, logger: logger}
}

func (r *MonitoredAddressRepository) AddMonitoredAddress(walletAddress string, chainID int) error {
	_, err := r.db.Exec(`
		INSERT INTO monitored_addresses (wallet_address, chain_id)
		VALUES ($1, $2)
		ON CONFLICT (wallet_address, chain_id) DO NOTHING
	`, walletAddress, chainID)

	if err != nil {
		return fmt.Errorf("failed to add monitored address: %w", err)
	}

	r.logger.Info("Added monitored address", 
		zap.String("wallet_address", walletAddress),
		zap.Int("chain_id", chainID))
	return nil
}

func (r *MonitoredAddressRepository) GetAllMonitoredAddresses() ([]model.MonitoredAddress, error) {
	rows, err := r.db.Query(`
		SELECT id, wallet_address, chain_id, created_at
		FROM monitored_addresses
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitored addresses: %w", err)
	}
	defer rows.Close()

	var addresses []model.MonitoredAddress
	for rows.Next() {
		var addr model.MonitoredAddress
		if err := rows.Scan(&addr.ID, &addr.WalletAddress, &addr.ChainID, &addr.CreatedAt); err != nil {
			return nil, fmt.Errorf("failed to scan monitored address: %w", err)
		}
		addresses = append(addresses, addr)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating monitored addresses: %w", err)
	}

	return addresses, nil
}

func (r *MonitoredAddressRepository) IsAddressMonitored(walletAddress string, chainID int) (bool, error) {
	var exists bool
	err := r.db.QueryRow(`
		SELECT EXISTS(SELECT 1 FROM monitored_addresses WHERE wallet_address = $1 AND chain_id = $2)
	`, walletAddress, chainID).Scan(&exists)

	if err != nil {
		return false, fmt.Errorf("failed to check if address is monitored: %w", err)
	}

	return exists, nil
}