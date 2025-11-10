import React, { useState, useEffect } from 'react';
import './App.css';

const ORDERS_API = 'http://localhost:8081/api';

// Fetch orders with status="preparing"
const fetchPreparingOrders = async () => {
  const response = await fetch(`${ORDERS_API}/orders?status=preparing`);
  if (!response.ok) {
    throw new Error(`Failed to fetch orders: ${response.statusText}`);
  }
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
        setOrders(preparingOrders);
      } catch (err) {
        console.error('Error loading orders:', err);
        setError('Fehler beim Laden der Bestellungen');
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
        headers: {
          'Content-Type': 'application/json'
        },
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
        <h1>👨‍🍳 Küchen Display</h1>
        <div className="order-count">
          {orders.length} {orders.length === 1 ? 'Bestellung' : 'Bestellungen'} in Zubereitung
        </div>
      </div>

      {error && (
        <div className="error-banner">
          ❌ {error}
        </div>
      )}

      {orders.length === 0 ? (
        <div className="empty-state">
          <div className="empty-icon">✅</div>
          <h2>Keine offenen Bestellungen</h2>
          <p>Alle Bestellungen sind fertig!</p>
        </div>
      ) : (
        <div className="orders-grid">
          {orders.map(order => (
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
        <h3>📝 Demo Hinweis</h3>
        <p>Um die Kitchen Display App vollständig zu testen:</p>
        <ol>
          <li>Erstelle eine Bestellung in der Customer App</li>
          <li>Bezahle via Stripe</li>
          <li>Die Bestellung erscheint automatisch hier mit Status "preparing"</li>
          <li>Klicke "Fertig" wenn die Bestellung bereit ist</li>
        </ol>
        <p><strong>Hinweis:</strong> Aktuell zeigt diese Demo keine Live-Orders an. In Production würde ein GET Endpoint benötigt werden: <code>GET /api/orders?status=preparing</code></p>
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
    if (!order.created_at) return '0 Min';
    const created = new Date(order.created_at);
    const diffMs = currentTime - created;
    const diffMins = Math.floor(diffMs / 60000);
    return `${diffMins} Min`;
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
          <span className="label">Bestellung</span>
          <span className="number">#{order.id?.slice(-6)}</span>
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
        <h3>Artikel:</h3>
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
        {loading ? '...' : '✓ FERTIG'}
      </button>
    </div>
  );
}

export default App;
