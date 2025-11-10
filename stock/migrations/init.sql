-- Stock Service Database Schema

-- Items Tabelle
CREATE TABLE IF NOT EXISTS items (
    id VARCHAR(50) PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    price_id VARCHAR(100) NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity >= 0),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- Index für schnelle Abfragen
CREATE INDEX IF NOT EXISTS idx_items_quantity ON items(quantity);


--  WICHTIG: Name muss mit Stripe Product Name übereinstimmen!
INSERT INTO items (id, name, price_id, quantity) VALUES
    ('1', 'Burger', 'price_1SQYsL3th7a1Jo3bsOVNnRpm', 1000),
    ('2', 'Pommes', 'price_POMMES_TODO', 1000)
ON CONFLICT (id) DO NOTHING;

-- Trigger für updated_at
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER update_items_updated_at
    BEFORE UPDATE ON items
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

