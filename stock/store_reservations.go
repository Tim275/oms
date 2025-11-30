package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	pb "github.com/timour/order-microservices/common/api"
)

// ReservationTTL defines how long a reservation stays active before expiring
const ReservationTTL = 15 * time.Minute

// =====================================================
// Inventory Reservation Methods
// =====================================================

// GetAvailableQuantity returns the available stock for an item
// Available = Total Quantity - Reserved Quantity
func (s *PostgresStore) GetAvailableQuantity(ctx context.Context, itemID string) (int32, error) {
	var availableQuantity int32

	query := `SELECT (quantity - reserved_quantity) AS available FROM items WHERE id = $1`
	err := s.db.QueryRowContext(ctx, query, itemID).Scan(&availableQuantity)

	if err == sql.ErrNoRows {
		return 0, fmt.Errorf("item not found: %s", itemID)
	}
	if err != nil {
		return 0, fmt.Errorf("failed to get available quantity: %w", err)
	}

	return availableQuantity, nil
}

// ReserveStock creates a reservation for multiple items (ACID transaction)
// This is called when an order is created (BEFORE payment)
//
// Flow:
// 1. Check if enough stock is available (quantity - reserved_quantity >= requested)
// 2. Increment reserved_quantity for each item
// 3. Insert reservation records into stock_reservations table
// 4. Set expiration time (NOW + 15 minutes)
//
// Returns: reservation_id (UUID) for tracking
func (s *PostgresStore) ReserveStock(ctx context.Context, orderID string, items []*pb.Item) (string, error) {
	// Generate unique reservation ID
	reservationID := uuid.New().String()
	expiresAt := time.Now().Add(ReservationTTL)

	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// Reserve each item
	for _, item := range items {
		// 1. Check if enough stock is available (atomic check + update)
		query := `
			UPDATE items
			SET reserved_quantity = reserved_quantity + $1,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
			  AND (quantity - reserved_quantity) >= $1
		`
		result, err := tx.ExecContext(ctx, query, item.Quantity, item.ID)
		if err != nil {
			return "", fmt.Errorf("failed to reserve stock for item %s: %w", item.ID, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return "", fmt.Errorf("failed to get rows affected: %w", err)
		}

		// If no rows were updated, insufficient stock
		if rowsAffected == 0 {
			return "", fmt.Errorf("insufficient stock for item %s (requested: %d)", item.ID, item.Quantity)
		}

		// 2. Insert reservation record
		insertQuery := `
			INSERT INTO stock_reservations
			(reservation_id, order_id, item_id, quantity, status, expires_at)
			VALUES ($1, $2, $3, $4, 'reserved', $5)
		`
		_, err = tx.ExecContext(ctx, insertQuery, reservationID, orderID, item.ID, item.Quantity, expiresAt)
		if err != nil {
			return "", fmt.Errorf("failed to insert reservation for item %s: %w", item.ID, err)
		}
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("failed to commit reservation transaction: %w", err)
	}

	return reservationID, nil
}

// ConfirmReservation converts a reservation into actual stock decrement (on payment success)
//
// Flow:
// 1. Find all reservations for the order
// 2. Decrement actual quantity for each item
// 3. Decrement reserved_quantity for each item
// 4. Mark reservations as 'confirmed'
//
// This is called when payment is successful
func (s *PostgresStore) ConfirmReservation(ctx context.Context, orderID string) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Get all reserved items for this order
	reservationsQuery := `
		SELECT item_id, quantity
		FROM stock_reservations
		WHERE order_id = $1 AND status = 'reserved'
	`
	rows, err := tx.QueryContext(ctx, reservationsQuery, orderID)
	if err != nil {
		return fmt.Errorf("failed to query reservations: %w", err)
	}
	defer rows.Close()

	type reservation struct {
		itemID   string
		quantity int32
	}

	var reservations []reservation
	for rows.Next() {
		var r reservation
		if err := rows.Scan(&r.itemID, &r.quantity); err != nil {
			return fmt.Errorf("failed to scan reservation: %w", err)
		}
		reservations = append(reservations, r)
	}

	if len(reservations) == 0 {
		return fmt.Errorf("no active reservations found for order %s", orderID)
	}

	// 2. Confirm each reservation
	for _, r := range reservations {
		// Update items: decrement both quantity and reserved_quantity
		updateItemsQuery := `
			UPDATE items
			SET quantity = quantity - $1,
			    reserved_quantity = reserved_quantity - $1,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2
			  AND reserved_quantity >= $1
			  AND quantity >= $1
		`
		result, err := tx.ExecContext(ctx, updateItemsQuery, r.quantity, r.itemID)
		if err != nil {
			return fmt.Errorf("failed to confirm reservation for item %s: %w", r.itemID, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			return fmt.Errorf("reservation mismatch for item %s (possibly already confirmed or released)", r.itemID)
		}
	}

	// 3. Mark all reservations as confirmed
	updateReservationsQuery := `
		UPDATE stock_reservations
		SET status = 'confirmed',
		    updated_at = CURRENT_TIMESTAMP
		WHERE order_id = $1 AND status = 'reserved'
	`
	_, err = tx.ExecContext(ctx, updateReservationsQuery, orderID)
	if err != nil {
		return fmt.Errorf("failed to update reservations status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit confirmation transaction: %w", err)
	}

	return nil
}

// ReleaseReservation releases a reservation (on payment failure or timeout)
//
// Flow:
// 1. Find all reservations for the order
// 2. Decrement reserved_quantity for each item (return to available stock)
// 3. Mark reservations as 'released'
//
// This is called when:
// - Payment fails
// - Order is cancelled
// - Reservation expires (background job)
func (s *PostgresStore) ReleaseReservation(ctx context.Context, orderID string) error {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Get all reserved items for this order
	reservationsQuery := `
		SELECT item_id, quantity
		FROM stock_reservations
		WHERE order_id = $1 AND status = 'reserved'
	`
	rows, err := tx.QueryContext(ctx, reservationsQuery, orderID)
	if err != nil {
		return fmt.Errorf("failed to query reservations: %w", err)
	}
	defer rows.Close()

	type reservation struct {
		itemID   string
		quantity int32
	}

	var reservations []reservation
	for rows.Next() {
		var r reservation
		if err := rows.Scan(&r.itemID, &r.quantity); err != nil {
			return fmt.Errorf("failed to scan reservation: %w", err)
		}
		reservations = append(reservations, r)
	}

	if len(reservations) == 0 {
		// No active reservations - might already be released/confirmed
		return nil
	}

	// 2. Release each reservation
	for _, r := range reservations {
		// Update items: decrement reserved_quantity only
		updateItemsQuery := `
			UPDATE items
			SET reserved_quantity = reserved_quantity - $1,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2 AND reserved_quantity >= $1
		`
		result, err := tx.ExecContext(ctx, updateItemsQuery, r.quantity, r.itemID)
		if err != nil {
			return fmt.Errorf("failed to release reservation for item %s: %w", r.itemID, err)
		}

		rowsAffected, err := result.RowsAffected()
		if err != nil {
			return fmt.Errorf("failed to get rows affected: %w", err)
		}

		if rowsAffected == 0 {
			return fmt.Errorf("reservation mismatch for item %s (possibly already released)", r.itemID)
		}
	}

	// 3. Mark all reservations as released
	updateReservationsQuery := `
		UPDATE stock_reservations
		SET status = 'released',
		    updated_at = CURRENT_TIMESTAMP
		WHERE order_id = $1 AND status = 'reserved'
	`
	_, err = tx.ExecContext(ctx, updateReservationsQuery, orderID)
	if err != nil {
		return fmt.Errorf("failed to update reservations status: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit release transaction: %w", err)
	}

	return nil
}

// CleanupExpiredReservations releases all reservations that have expired
// This is called by a background job every minute
//
// Returns: number of reservations cleaned up
func (s *PostgresStore) CleanupExpiredReservations(ctx context.Context) (int, error) {
	// Start transaction
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback()

	// 1. Find all expired reservations
	reservationsQuery := `
		SELECT order_id, item_id, quantity
		FROM stock_reservations
		WHERE status = 'reserved'
		  AND expires_at < NOW()
	`
	rows, err := tx.QueryContext(ctx, reservationsQuery)
	if err != nil {
		return 0, fmt.Errorf("failed to query expired reservations: %w", err)
	}
	defer rows.Close()

	type expiredReservation struct {
		orderID  string
		itemID   string
		quantity int32
	}

	var expired []expiredReservation
	for rows.Next() {
		var e expiredReservation
		if err := rows.Scan(&e.orderID, &e.itemID, &e.quantity); err != nil {
			return 0, fmt.Errorf("failed to scan expired reservation: %w", err)
		}
		expired = append(expired, e)
	}

	if len(expired) == 0 {
		return 0, nil
	}

	// 2. Release each expired reservation
	for _, e := range expired {
		// Update items: decrement reserved_quantity
		updateItemsQuery := `
			UPDATE items
			SET reserved_quantity = reserved_quantity - $1,
			    updated_at = CURRENT_TIMESTAMP
			WHERE id = $2 AND reserved_quantity >= $1
		`
		_, err := tx.ExecContext(ctx, updateItemsQuery, e.quantity, e.itemID)
		if err != nil {
			return 0, fmt.Errorf("failed to release expired reservation for item %s: %w", e.itemID, err)
		}
	}

	// 3. Mark all expired reservations as 'expired'
	updateReservationsQuery := `
		UPDATE stock_reservations
		SET status = 'expired',
		    updated_at = CURRENT_TIMESTAMP
		WHERE status = 'reserved'
		  AND expires_at < NOW()
	`
	result, err := tx.ExecContext(ctx, updateReservationsQuery)
	if err != nil {
		return 0, fmt.Errorf("failed to update expired reservations: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("failed to commit cleanup transaction: %w", err)
	}

	return int(rowsAffected), nil
}
