import React, { useState, useEffect } from 'react';
import './App.css';

const API_BASE = 'http://localhost:8081/api';

const STATUS_COLORS = {
  preparing: '#2196F3',
  ready: '#FF5722'
};

const STATUS_LABELS = {
  preparing: 'In Zubereitung',
  ready: 'Bereit zur Abholung!'
};

function App() {
  const [customerId] = useState(() => `ORDER_${Date.now()}`); // Auto-generate like McDonald's
  const [menuItems, setMenuItems] = useState([]); // ⭐ Fetch from /api/menu
  const [orderItems, setOrderItems] = useState({});
  const [currentOrder, setCurrentOrder] = useState(null);
  const [loading, setLoading] = useState(false);
  const [loadingMenu, setLoadingMenu] = useState(true); // ⭐ Loading state for menu
  const [error, setError] = useState(null);

  // ⭐ Fetch menu from backend on component mount
  useEffect(() => {
    const fetchMenu = async () => {
      try {
        setLoadingMenu(true);
        const response = await fetch(`${API_BASE}/menu`);
        if (!response.ok) throw new Error('Failed to fetch menu');
        const items = await response.json();
        setMenuItems(items);
      } catch (err) {
        console.error('Error fetching menu:', err);
        setError('Failed to load menu. Please refresh the page.');
      } finally {
        setLoadingMenu(false);
      }
    };

    fetchMenu();
  }, []);

  // Poll for order status updates every 3 seconds
  useEffect(() => {
    if (!currentOrder?.id || !customerId) return;

    const interval = setInterval(async () => {
      try {
        const response = await fetch(`${API_BASE}/customers/${customerId}/orders/${currentOrder.id}`);
        if (!response.ok) throw new Error('Failed to fetch order status');
        const order = await response.json();
        setCurrentOrder(order);
      } catch (err) {
        console.error('Error polling order status:', err);
      }
    }, 3000);

    return () => clearInterval(interval);
  }, [currentOrder?.id, customerId]);

  const handleQuantityChange = (itemId, quantity) => {
    setOrderItems(prev => ({
      ...prev,
      [itemId]: Math.max(0, quantity)
    }));
  };

  const handleCreateOrder = async (e) => {
    e.preventDefault();
    setError(null);
    setLoading(true);

    try {
      // Build items array for API with price_id from menu
      const items = Object.entries(orderItems)
        .filter(([_, qty]) => qty > 0)
        .map(([id, quantity]) => {
          const menuItem = menuItems.find(item => item.id === id);
          return {
            id: id,
            quantity: quantity,
            price_id: menuItem?.priceId || '' // ⭐ Include Stripe Price ID
          };
        });

      if (items.length === 0) {
        throw new Error('Bitte mindestens ein Item auswählen');
      }

      const response = await fetch(`${API_BASE}/customers/${customerId}/orders`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(items)
      });

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Order creation failed: ${errorText}`);
      }

      const order = await response.json();
      setCurrentOrder(order);
      setOrderItems({});
    } catch (err) {
      setError(err.message);
    } finally {
      setLoading(false);
    }
  };

  const handleNewOrder = () => {
    setCurrentOrder(null);
    setOrderItems({});
    setError(null);
  };

  const calculateTotal = () => {
    return Object.entries(orderItems)
      .reduce((sum, [itemId, qty]) => {
        const item = menuItems.find(m => m.id === itemId);
        return sum + (item ? item.price * qty : 0);
      }, 0);
  };

  if (currentOrder) {
    return (
      <div className="App">
        <div className="container">
          <h1>🍔 Bestellung #{currentOrder.id?.slice(-6)}</h1>

          {(currentOrder.status === 'preparing' || currentOrder.status === 'ready') && (
            <div className="order-status">
              <div
                className="status-badge"
                style={{ backgroundColor: STATUS_COLORS[currentOrder.status] }}
              >
                {STATUS_LABELS[currentOrder.status]}
              </div>
            </div>
          )}

          {currentOrder.status === 'ready' && (
            <div className="ready-notification">
              🎉 Ihre Bestellung ist bereit zur Abholung!
            </div>
          )}

          <div className="order-details">
            <h2>Bestelldetails</h2>
            <div className="items-list">
              {currentOrder.items?.map((item, idx) => (
                <div key={idx} className="item-row">
                  <span>{item.Name}</span>
                  <span>x{item.Quantity}</span>
                </div>
              ))}
            </div>
          </div>

          {currentOrder.payment_link && currentOrder.status === 'waiting_payment' && (
            <div className="payment-section">
              <h2>Zahlung</h2>
              <a
                href={currentOrder.payment_link}
                target="_blank"
                rel="noopener noreferrer"
                className="payment-button"
              >
                💳 Jetzt bezahlen (Stripe)
              </a>
            </div>
          )}

          {(currentOrder.status === 'preparing' || currentOrder.status === 'ready') && (
            <div className="status-timeline">
              <h3>Status-Verlauf</h3>
              <div className="timeline">
                {Object.keys(STATUS_LABELS).map(status => (
                  <div
                    key={status}
                    className={`timeline-step ${
                      Object.keys(STATUS_LABELS).indexOf(currentOrder.status) >= Object.keys(STATUS_LABELS).indexOf(status)
                        ? 'active'
                        : ''
                    }`}
                  >
                    <div className="timeline-dot"></div>
                    <div className="timeline-label">{STATUS_LABELS[status]}</div>
                  </div>
                ))}
              </div>
            </div>
          )}

          <button onClick={handleNewOrder} className="new-order-button">
            Neue Bestellung
          </button>
        </div>
      </div>
    );
  }

  // Show loading state while fetching menu
  if (loadingMenu) {
    return (
      <div className="App">
        <div className="container">
          <h1>🍔 Burger Bestellung</h1>
          <div className="error-message" style={{ background: '#FFBC0D', color: '#292929' }}>
            ⏳ Menü wird geladen...
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="App">
      <div className="container">
        <h1>🍔 Burger Bestellung</h1>

        {error && (
          <div className="error-message">
            ❌ {error}
          </div>
        )}

        <form onSubmit={handleCreateOrder}>
          <div className="menu-section">
            <h2>Wähle deine Produkte</h2>
            <div className="menu-grid">
              {menuItems.map(item => {
                const isOutOfStock = item.quantity === 0;
                return (
                  <div key={item.id} className={`menu-item ${isOutOfStock ? 'out-of-stock' : ''}`}>
                    <img src={item.image} alt={item.name} className="item-image" />
                    <div className="item-info">
                      <div className="item-name-row">
                        <span className="item-name">{item.name}</span>
                        {isOutOfStock && <span className="out-of-stock-badge">Out of Stock</span>}
                      </div>
                      <span className="item-description">{item.description}</span>
                      <span className="item-price">€{item.price.toFixed(2)}</span>
                    </div>
                    <div className="quantity-controls">
                      <button
                        type="button"
                        onClick={() => handleQuantityChange(item.id, (orderItems[item.id] || 0) - 1)}
                        disabled={!orderItems[item.id] || orderItems[item.id] === 0 || isOutOfStock}
                      >
                        −
                      </button>
                      <span className="quantity">{orderItems[item.id] || 0}</span>
                      <button
                        type="button"
                        onClick={() => handleQuantityChange(item.id, (orderItems[item.id] || 0) + 1)}
                        disabled={isOutOfStock}
                      >
                        +
                      </button>
                    </div>
                  </div>
                );
              })}
            </div>
          </div>

          <div className="total-section">
            <span>Gesamt:</span>
            <span className="total-amount">€{calculateTotal().toFixed(2)}</span>
          </div>

          <button
            type="submit"
            className="submit-button"
            disabled={loading || calculateTotal() === 0}
          >
            {loading ? 'Bestellung wird erstellt...' : 'Bestellung aufgeben'}
          </button>
        </form>
      </div>
    </div>
  );
}

export default App;
