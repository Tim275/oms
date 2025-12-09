package main

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/XSAM/otelsql"
	"github.com/lib/pq"
	pb "github.com/timour/order-microservices/common/api"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"
)

// PostgresStore implementiert Store Interface mit PostgreSQL
type PostgresStore struct {
	db *sql.DB
}

// NewPostgresStore erstellt eine neue PostgreSQL Store Instanz
// Cloud-Native: Auto-Migration bei Startup (Application owns its schema)
// OpenTelemetry: Traces für alle SQL Queries via otelsql
func NewPostgresStore(connectionString string) (*PostgresStore, error) {
	// Register the otelsql wrapper for the postgres driver
	// This enables automatic tracing for all database operations
	driverName, err := otelsql.Register("postgres",
		otelsql.WithAttributes(semconv.DBSystemPostgreSQL),
		otelsql.WithSpanOptions(otelsql.SpanOptions{
			Ping:     true,
			RowsNext: false, // Avoid noise from row iteration
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to register otelsql driver: %w", err)
	}

	db, err := sql.Open(driverName, connectionString)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Register DB stats metrics for Prometheus/OpenTelemetry
	if err := otelsql.RegisterDBStatsMetrics(db, otelsql.WithAttributes(semconv.DBSystemPostgreSQL)); err != nil {
		return nil, fmt.Errorf("failed to register db stats metrics: %w", err)
	}

	// Test connection
	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	store := &PostgresStore{db: db}

	// Auto-Migration: Schema wird bei jedem Start geprüft/erstellt
	if err := store.runMigrations(); err != nil {
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return store, nil
}

// runMigrations führt Schema-Migrationen beim Startup aus
// Cloud-Native Best Practice: Application owns its database schema
func (s *PostgresStore) runMigrations() error {
	// Create items table if not exists
	schema := `
	CREATE TABLE IF NOT EXISTS items (
		id VARCHAR(50) PRIMARY KEY,
		name VARCHAR(100) NOT NULL,
		price_id VARCHAR(100) NOT NULL,
		quantity INTEGER NOT NULL DEFAULT 100 CHECK (quantity >= 0),
		reserved_quantity INTEGER NOT NULL DEFAULT 0,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);

	-- Index für schnelle Abfragen
	CREATE INDEX IF NOT EXISTS idx_items_quantity ON items(quantity);

	-- Stock Reservations Table für 2-Phase Commit Pattern
	CREATE TABLE IF NOT EXISTS stock_reservations (
		id SERIAL PRIMARY KEY,
		reservation_id VARCHAR(255) NOT NULL,
		order_id VARCHAR(255) NOT NULL,
		item_id VARCHAR(50) NOT NULL REFERENCES items(id) ON DELETE RESTRICT,
		quantity INTEGER NOT NULL CHECK (quantity > 0),
		status VARCHAR(50) NOT NULL CHECK (status IN ('reserved', 'confirmed', 'released', 'expired')),
		expires_at TIMESTAMP NOT NULL,
		created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(reservation_id, item_id)
	);

	-- Indexes for stock_reservations
	CREATE INDEX IF NOT EXISTS idx_reservations_order_id ON stock_reservations(order_id);
	CREATE INDEX IF NOT EXISTS idx_reservations_item_status ON stock_reservations(item_id, status);

	-- Seed default menu items (ON CONFLICT DO NOTHING = idempotent)
	-- Only items with valid Stripe price_ids
	INSERT INTO items (id, name, price_id, quantity) VALUES
		('1', 'Burger', 'price_1SQYsL3th7a1Jo3bsOVNnRpm', 1000),
		('2', 'Pommes', 'price_1SRMZL3th7a1Jo3b5LNJkEoe', 1000)
	ON CONFLICT (id) DO NOTHING;

	-- Remove items without valid Stripe price_ids
	DELETE FROM items WHERE id IN ('3', '4', '5');
	`

	_, err := s.db.Exec(schema)
	if err != nil {
		return fmt.Errorf("migration failed: %w", err)
	}

	return nil
}

// Close schließt die Datenbankverbindung
func (s *PostgresStore) Close() error {
	return s.db.Close()
}

// GetItem ruft ein einzelnes Item aus der Datenbank ab
func (s *PostgresStore) GetItem(ctx context.Context, id string) (*pb.Item, error) {
	var item pb.Item

	query := `SELECT id, name, price_id, quantity FROM items WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&item.ID,
		&item.Name,
		&item.PriceID,
		&item.Quantity,
	)

	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("item not found")
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get item: %w", err)
	}

	return &item, nil
}

// GetItems ruft mehrere Items aus der Datenbank ab
// Wenn ids leer ist, werden ALLE Items zurückgegeben
func (s *PostgresStore) GetItems(ctx context.Context, ids []string) ([]*pb.Item, error) {
	var rows *sql.Rows
	var err error

	// If no IDs specified, return ALL items
	if len(ids) == 0 {
		query := `SELECT id, name, price_id, quantity FROM items ORDER BY id`
		rows, err = s.db.QueryContext(ctx, query)
	} else {
		// Build query with placeholders for specific IDs
		query := `SELECT id, name, price_id, quantity FROM items WHERE id = ANY($1)`
		rows, err = s.db.QueryContext(ctx, query, pq.Array(ids))
	}

	if err != nil {
		return nil, fmt.Errorf("failed to query items: %w", err)
	}
	defer rows.Close()

	var items []*pb.Item
	for rows.Next() {
		var item pb.Item
		if err := rows.Scan(&item.ID, &item.Name, &item.PriceID, &item.Quantity); err != nil {
			return nil, fmt.Errorf("failed to scan item: %w", err)
		}
		items = append(items, &item)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows error: %w", err)
	}

	return items, nil
}

// UpdateQuantity aktualisiert die Quantity eines Items (für spätere Features)
func (s *PostgresStore) UpdateQuantity(ctx context.Context, id string, quantity int32) error {
	query := `UPDATE items SET quantity = $1, updated_at = CURRENT_TIMESTAMP WHERE id = $2`
	result, err := s.db.ExecContext(ctx, query, quantity, id)
	if err != nil {
		return fmt.Errorf("failed to update quantity: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("item not found")
	}

	return nil
}

// DecrementQuantity reduziert die Quantity eines Items (für Order Processing)
func (s *PostgresStore) DecrementQuantity(ctx context.Context, id string, amount int32) error {
	query := `
		UPDATE items
		SET quantity = quantity - $1, updated_at = CURRENT_TIMESTAMP
		WHERE id = $2 AND quantity >= $1
	`
	result, err := s.db.ExecContext(ctx, query, amount, id)
	if err != nil {
		return fmt.Errorf("failed to decrement quantity: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return fmt.Errorf("insufficient stock or item not found")
	}

	return nil
}
