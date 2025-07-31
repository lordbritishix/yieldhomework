package repository

import (
	"database/sql"
	"fmt"
	"go.uber.org/zap"
	"yield/apps/yield/internal/model"
)

type OrderRepository struct {
	db     *sql.DB
	logger *zap.Logger
}

func NewOrderRepository(db *sql.DB, logger *zap.Logger) *OrderRepository {
	return &OrderRepository{db: db, logger: logger}
}

func (r *OrderRepository) UpsertOrder(order model.Order) error {
	_, err := r.db.Exec(`
		INSERT INTO orders (order_id, tx_hash, log_index, block_number, tx_date, transfer_type, status, wallet_address, amount, from_asset_name, to_asset_name, estimated_amount)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (tx_hash, log_index) DO UPDATE SET
			order_id = EXCLUDED.order_id,
			block_number = EXCLUDED.block_number,
			tx_date = EXCLUDED.tx_date,
			transfer_type = EXCLUDED.transfer_type,
			status = EXCLUDED.status,
			wallet_address = EXCLUDED.wallet_address,
			amount = EXCLUDED.amount,
			from_asset_name = EXCLUDED.from_asset_name,
			to_asset_name = EXCLUDED.to_asset_name,
			estimated_amount = EXCLUDED.estimated_amount
	`, order.OrderID, order.TxHash, order.LogIndex, order.BlockNumber, order.TxDate, order.TransferType, order.Status, order.WalletAddress, order.Amount, order.FromAssetName, order.ToAssetName, order.EstimatedAmount)

	if err != nil {
		return fmt.Errorf("failed to upsert order: %w", err)
	}

	r.logger.Info("Upserted order",
		zap.String("tx_hash", order.TxHash),
		zap.Uint64("log_index", order.LogIndex),
		zap.String("transfer_type", order.TransferType),
		zap.String("status", order.Status),
		zap.String("wallet_address", order.WalletAddress))
	return nil
}

func (r *OrderRepository) GetOrderByTxHash(txHash string) (*model.Order, error) {
	var order model.Order
	err := r.db.QueryRow(`
		SELECT order_id, tx_hash, log_index, block_number, tx_date, transfer_type, status, wallet_address, amount, from_asset_name, to_asset_name, estimated_amount
		FROM orders 
		WHERE tx_hash = $1
	`, txHash).Scan(&order.OrderID, &order.TxHash, &order.LogIndex, &order.BlockNumber, &order.TxDate, &order.TransferType,
		&order.Status, &order.WalletAddress, &order.Amount, &order.FromAssetName, &order.ToAssetName, &order.EstimatedAmount)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	return &order, nil
}

func (r *OrderRepository) GetLastInProgressWithdrawalByWallet(walletAddress string) (*model.Order, error) {
	var order model.Order
	err := r.db.QueryRow(`
		SELECT order_id, tx_hash, log_index, block_number, tx_date, transfer_type, status, wallet_address, amount, from_asset_name, to_asset_name, estimated_amount
		FROM orders 
		WHERE wallet_address = $1 AND transfer_type = 'withdrawal' AND status = 'in_progress'
		ORDER BY tx_date DESC
		LIMIT 1
	`, walletAddress).Scan(&order.OrderID, &order.TxHash, &order.LogIndex, &order.BlockNumber, &order.TxDate, &order.TransferType,
		&order.Status, &order.WalletAddress, &order.Amount, &order.FromAssetName, &order.ToAssetName, &order.EstimatedAmount)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get last in_progress withdrawal: %w", err)
	}

	return &order, nil
}

func (r *OrderRepository) GetInProgressWithdrawalByWalletAndAmount(walletAddress string, amount string) (*model.Order, error) {
	var order model.Order
	err := r.db.QueryRow(`
		SELECT order_id, tx_hash, log_index, block_number, tx_date, transfer_type, status, wallet_address, amount, from_asset_name, to_asset_name, estimated_amount
		FROM orders 
		WHERE wallet_address = $1 AND transfer_type = 'withdrawal' AND status = 'in_progress' AND amount = $2
		ORDER BY tx_date DESC
		LIMIT 1
	`, walletAddress, amount).Scan(&order.OrderID, &order.TxHash, &order.LogIndex, &order.BlockNumber, &order.TxDate, &order.TransferType,
		&order.Status, &order.WalletAddress, &order.Amount, &order.FromAssetName, &order.ToAssetName, &order.EstimatedAmount)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get in_progress withdrawal by wallet and amount: %w", err)
	}

	return &order, nil
}

func (r *OrderRepository) GetOrderByID(orderID string) (*model.Order, error) {
	var order model.Order
	err := r.db.QueryRow(`
		SELECT order_id, tx_hash, log_index, block_number, tx_date, transfer_type, status, wallet_address, amount, from_asset_name, to_asset_name, estimated_amount
		FROM orders 
		WHERE order_id = $1
	`, orderID).Scan(&order.OrderID, &order.TxHash, &order.LogIndex, &order.BlockNumber, &order.TxDate, &order.TransferType,
		&order.Status, &order.WalletAddress, &order.Amount, &order.FromAssetName, &order.ToAssetName, &order.EstimatedAmount)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get order by ID: %w", err)
	}

	return &order, nil
}

func (r *OrderRepository) CreateOrder(order model.Order) error {
	_, err := r.db.Exec(`
		INSERT INTO orders (order_id, tx_hash, log_index, block_number, tx_date, transfer_type, status, wallet_address, amount, from_asset_name, to_asset_name, estimated_amount)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, order.OrderID, order.TxHash, order.LogIndex, order.BlockNumber, order.TxDate, order.TransferType, order.Status, order.WalletAddress, order.Amount, order.FromAssetName, order.ToAssetName, order.EstimatedAmount)

	if err != nil {
		return fmt.Errorf("failed to create order: %w", err)
	}

	r.logger.Info("Created order",
		zap.String("order_id", order.OrderID),
		zap.String("transfer_type", order.TransferType),
		zap.String("status", order.Status),
		zap.String("wallet_address", order.WalletAddress))
	return nil
}

func (r *OrderRepository) UpdateOrderStatus(txHash, status string) error {
	_, err := r.db.Exec(`
		UPDATE orders SET status = $1 WHERE tx_hash = $2
	`, status, txHash)

	if err != nil {
		return fmt.Errorf("failed to update order status: %w", err)
	}

	r.logger.Info("Updated order status",
		zap.String("tx_hash", txHash),
		zap.String("status", status))
	return nil
}
