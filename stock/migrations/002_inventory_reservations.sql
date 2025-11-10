
ALTER TABLE items
ADD COLUMN reserved_quantity INTEGER NOT NULL DEFAULT 0;

ALTER TABLE items
ADD CONSTRAINT items_reserved_check CHECK (reserved_quantity >= 0 AND reserved_quantity <= quantity);

CREATE INDEX idx_items_available_stock ON items((quantity - reserved_quantity));


CREATE TABLE stock_reservations (
    id SERIAL PRIMARY KEY,
    reservation_id VARCHAR(255) UNIQUE NOT NULL,  -- UUID for idempotency
    order_id VARCHAR(255) NOT NULL,               -- Link to order
    item_id VARCHAR(50) NOT NULL,                 -- Link to item
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    status VARCHAR(50) NOT NULL CHECK (status IN ('reserved', 'confirmed', 'released', 'expired')),
    expires_at TIMESTAMP NOT NULL,                -- TTL for auto-release (15 minutes)
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,

    -- Foreign key to items table
    FOREIGN KEY (item_id) REFERENCES items(id) ON DELETE RESTRICT
);

-- =====================================================
-- PART 3: Indexes for performance
-- =====================================================

-- Index for finding reservations by order (for confirm/release)
CREATE INDEX idx_reservations_order_id ON stock_reservations(order_id);

-- Index for finding expired reservations (for background cleanup job)
CREATE INDEX idx_reservations_expires ON stock_reservations(expires_at, status)
WHERE status = 'reserved';

-- Index for finding active reservations by item
CREATE INDEX idx_reservations_item_status ON stock_reservations(item_id, status);

-- =====================================================
-- PART 4: Trigger for updated_at
-- =====================================================

CREATE TRIGGER update_stock_reservations_updated_at
    BEFORE UPDATE ON stock_reservations
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =====================================================
-- PART 5: Helper View - Available Stock
-- =====================================================

CREATE VIEW items_available_stock AS
SELECT
    id,
    name,
    price_id,
    quantity AS total_quantity,
    reserved_quantity,
    (quantity - reserved_quantity) AS available_quantity,
    created_at,
    updated_at
FROM items;

