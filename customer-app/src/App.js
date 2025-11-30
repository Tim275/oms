import React, { useState, useEffect } from 'react';
import './App.css';

// Dynamic API URL: Works with both localhost (port-forward) and production domain
const getApiBase = () => {
  const hostname = window.location.hostname;

  // localhost or 127.0.0.1 → use port-forward URL
  if (hostname === 'localhost' || hostname === '127.0.0.1') {
    return 'http://localhost:8080/api';
  }

  // oms.timourhomelab.org → use oms-api.timourhomelab.org
  if (hostname.includes('timourhomelab.org')) {
    const protocol = window.location.protocol; // http: or https:
    return `${protocol}//oms-api.timourhomelab.org/api`;
  }

  // Fallback: use env variable or default
  return process.env.REACT_APP_API_URL || 'http://localhost:8080/api';
};

const API_BASE = getApiBase();

const STATUS_COLORS = {
  preparing: '#2196F3',
  ready: '#FF5722'
};

const STATUS_LABELS = {
  preparing: 'Being Prepared',
  ready: 'Ready for Pickup!'
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

    // Build items array for validation
    const items = Object.entries(orderItems)
      .filter(([_, qty]) => qty > 0)
      .map(([id, quantity]) => {
        const menuItem = menuItems.find(item => item.id === id);
        return {
          id: id,
          quantity: quantity,
          price_id: menuItem?.priceId || '',
          name: menuItem?.name || ''
        };
      });

    if (items.length === 0) {
      setError('Please select at least one item');
      return;
    }

    setLoading(true);

    try {
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
    // Show receipt ONLY after payment (preparing or ready)
    const showReceipt = currentOrder.status === 'preparing' || currentOrder.status === 'ready';

    return (
      <div className="App">
        <div className="container">
          {showReceipt ? (
            <>
              {currentOrder.status === 'preparing' && (
                <>
                  <div className="order-status">
                    <div className="status-badge" style={{ backgroundColor: STATUS_COLORS.preparing }}>
                      🍳 Order #{currentOrder.order_number} is being prepared
                    </div>
                  </div>

                  {/* Receipt only shown when "preparing" */}
                  <div className="printable-receipt">
                    <div className="receipt-section">
                      <h2>📋 Receipt & Pickup Slip</h2>

                      {/* Pickup Slip - Order Number */}
                      <div className="receipt-order-number">
                        <div className="receipt-number-label">Order Number</div>
                        <div className="receipt-number-big">#{currentOrder.order_number || '?'}</div>
                        <div className="receipt-number-note">Please show this number when picking up</div>
                      </div>

                      {/* Receipt - Items */}
                      <div className="receipt-items">
                        <h3>Your Order:</h3>
                        {currentOrder.items?.map((item, idx) => {
                          const menuItem = menuItems.find(m => m.id === item.ID);
                          const itemPrice = menuItem ? menuItem.price : 0;
                          const totalPrice = itemPrice * item.Quantity;
                          return (
                            <div key={idx} className="receipt-item-row">
                              <span className="item-qty">{item.Quantity}x</span>
                              <span className="item-name">{item.Name}</span>
                              <span className="item-price">€{itemPrice.toFixed(2)}</span>
                              <span className="item-total">€{totalPrice.toFixed(2)}</span>
                            </div>
                          );
                        })}
                        <div className="receipt-divider"></div>
                        <div className="receipt-total">
                          <span>Total:</span>
                          <span className="total-amount">
                            €{currentOrder.items?.reduce((sum, item) => {
                              const menuItem = menuItems.find(m => m.id === item.ID);
                              const itemPrice = menuItem ? menuItem.price : 0;
                              return sum + (itemPrice * item.Quantity);
                            }, 0).toFixed(2)}
                          </span>
                        </div>
                      </div>
                      <button onClick={() => window.print()} className="print-button">
                        🖨️ Print Receipt
                      </button>
                    </div>
                  </div>
                </>
              )}

              {currentOrder.status === 'ready' && (
                <div className="ready-pickup">
                  <div className="ready-icon">🎉</div>
                  <h1>Order #{currentOrder.order_number} is ready!</h1>
                  <p className="pickup-message">Please pick up at the counter</p>
                </div>
              )}
            </>
          ) : (
            <>
              <div className="success-message">
                <h1>Processing your order...</h1>
                <div className="processing-indicator">
                  <div className="spinner"></div>
                  <p>Please wait a moment</p>
                </div>
              </div>
            </>
          )}

          <div className="order-details">
            <h2>Order Details</h2>
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
              <h2>Payment</h2>
              <a
                href={currentOrder.payment_link}
                target="_blank"
                rel="noopener noreferrer"
                className="payment-button"
              >
                💳 Pay Now (Stripe)
              </a>
            </div>
          )}

          {(currentOrder.status === 'preparing' || currentOrder.status === 'ready') && (
            <div className="status-timeline">
              <h3>Status Timeline</h3>
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
            New Order
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
          <h1>🍔 Burger Order</h1>
          <div className="error-message" style={{ background: '#FFBC0D', color: '#292929' }}>
            ⏳ Loading menu...
          </div>
        </div>
      </div>
    );
  }

  return (
    <div className="App">
      <div className="container">
        <h1>🍔 Burger Order</h1>

        {error && (
          <div className="error-message">
            ❌ {error}
          </div>
        )}

        <form onSubmit={handleCreateOrder}>
          <div className="menu-section">
            <h2>Choose your items</h2>
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
            <span>Total:</span>
            <span className="total-amount">€{calculateTotal().toFixed(2)}</span>
          </div>

          <button
            type="submit"
            className="submit-button"
            disabled={loading || calculateTotal() === 0}
          >
            {loading ? 'Creating order...' : 'Place Order'}
          </button>
        </form>
      </div>
    </div>
  );
}

export default App;
