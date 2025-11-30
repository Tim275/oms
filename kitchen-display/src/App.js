import React, { useState, useEffect } from 'react';
import './App.css';

// Dynamic API URL: Works with both localhost (port-forward) and production domain
const getApiBase = () => {
  const hostname = window.location.hostname;

  // localhost or 127.0.0.1 → use port-forward URL
  if (hostname === 'localhost' || hostname === '127.0.0.1') {
    return 'http://localhost:8080/api';
  }

  // oms-kitchen.timourhomelab.org → use oms-api.timourhomelab.org
  if (hostname.includes('timourhomelab.org')) {
    const protocol = window.location.protocol; // http: or https:
    return `${protocol}//oms-api.timourhomelab.org/api`;
  }

  // Fallback: use env variable or default
  return process.env.REACT_APP_API_URL || 'http://localhost:8080/api';
};

const ORDERS_API = getApiBase();

const fetchPreparingOrders = async () => {
  const response = await fetch(`${ORDERS_API}/orders?status=preparing`);
  if (!response.ok) throw new Error(`Failed to fetch orders: ${response.statusText}`);
  return await response.json();
};

function App() {
  const [orders, setOrders] = useState([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState(null);

  // Poll for orders every 5 seconds
  useEffect(() => {
    const loadOrders = async () => {
      try {
        const preparingOrders = await fetchPreparingOrders();
        // Handle null/undefined response - default to empty array
        setOrders(preparingOrders || []);
      } catch (err) {
        console.error('Error loading orders:', err);
        setError('Error loading orders');
      }
    };

    loadOrders();
    const interval = setInterval(loadOrders, 5000);
    return () => clearInterval(interval);
  }, []);

  const handleMarkReady = async (orderId, customerId) => {
    setLoading(true);
    setError(null);

    try {
      const response = await fetch(`${ORDERS_API}/customers/${customerId}/orders/${orderId}`, {
        method: 'PUT',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ status: 'ready' })
      });

      if (!response.ok) {
        const errorText = await response.text();
        throw new Error(`Failed to mark order ready: ${errorText}`);
      }

      const result = await response.json();
      console.log('Order marked ready:', result);

      // Remove order from list (it's now "ready" status)
      setOrders(prev => prev.filter(o => o.id !== orderId));
    } catch (err) {
      setError(err.message);
      console.error('Error marking order ready:', err);
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="App">
      <div className="kitchen-header">
        <h1>👨‍🍳 Kitchen Display</h1>
        <div className="order-count">
          {(orders || []).length} {(orders || []).length === 1 ? 'order' : 'orders'} being prepared
        </div>
      </div>

      {error && (
        <div className="error-banner">
          ❌ {error}
        </div>
      )}

      {(!orders || orders.length === 0) ? (
        <div className="empty-state">
          <div className="empty-icon">✅</div>
          <h2>No open orders</h2>
          <p>All orders are ready!</p>
        </div>
      ) : (
        <div className="orders-grid">
          {(orders || []).map(order => (
            <OrderCard
              key={order.id}
              order={order}
              onMarkReady={handleMarkReady}
              loading={loading}
            />
          ))}
        </div>
      )}

      <div className="demo-info">
        <h3>📝 Demo Note</h3>
        <p>To fully test the Kitchen Display App:</p>
        <ol>
          <li>Create an order in the Customer App</li>
          <li>Pay via Stripe</li>
          <li>The order will automatically appear here with status "preparing"</li>
          <li>Click "READY" when the order is done</li>
        </ol>
        <p><strong>Note:</strong> Currently this demo doesn't show live orders. In production a GET endpoint would be needed: <code>GET /api/orders?status=preparing</code></p>
      </div>
    </div>
  );
}

function OrderCard({ order, onMarkReady, loading }) {
  const [currentTime, setCurrentTime] = useState(new Date());

  // Update timer every second
  useEffect(() => {
    const timer = setInterval(() => {
      setCurrentTime(new Date());
    }, 1000);
    return () => clearInterval(timer);
  }, []);

  const getTimeSinceCreated = () => {
    if (!order.created_at) return '0 min';
    const created = new Date(order.created_at);
    const diffMs = currentTime - created;
    const diffMins = Math.floor(diffMs / 60000);
    return `${diffMins} min`;
  };

  const getUrgencyClass = () => {
    if (!order.created_at) return '';
    const created = new Date(order.created_at);
    const diffMs = currentTime - created;
    const diffMins = Math.floor(diffMs / 60000);

    if (diffMins > 15) return 'urgent';
    if (diffMins > 10) return 'warning';
    return 'fresh';
  };

  return (
    <div className={`order-card ${getUrgencyClass()}`}>
      <div className="order-card-header">
        <div className="order-number">
          <span className="label">Order</span>
          <span className="number">#{order.order_number || '?'}</span>
        </div>
        <div className="order-time">
          ⏱️ {getTimeSinceCreated()}
        </div>
      </div>

      <div className="order-customer">
        <span className="customer-icon">👤</span>
        {order.customer_id}
      </div>

      <div className="order-items">
        <h3>Items:</h3>
        {order.items?.map((item, idx) => (
          <div key={idx} className="order-item">
            <span className="item-quantity">{item.Quantity}x</span>
            <span className="item-name">{item.Name}</span>
          </div>
        ))}
      </div>

      <button
        onClick={() => onMarkReady(order.id, order.customer_id)}
        className="ready-button"
        disabled={loading}
      >
        {loading ? '...' : '✓ READY'}
      </button>
    </div>
  );
}

export default App;
