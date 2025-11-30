package processor

import (
	"fmt"
	"log"
	"os"

	pb "github.com/timour/order-microservices/common/api"
	"github.com/stripe/stripe-go/v78"
	"github.com/stripe/stripe-go/v78/checkout/session"
)

// Warum Stripe struct?
// → Kapselt Stripe API Key
// → Könnte später erweitert werden (Mock für Tests, etc.)
type Stripe struct {
	apiKey string
}

// Warum stripe.Key = apiKey?
// → Setzt GLOBALEN API Key für Stripe SDK
// → Alle Stripe API Calls nutzen diesen Key
func NewStripeProcessor(apiKey string) *Stripe {
	stripe.Key = apiKey
	return &Stripe{
		apiKey: apiKey,
	}
}

// CreatePaymentLink: Erstellt Stripe Checkout Session
// Warum Checkout Session (nicht Payment Intent)?
// → Checkout Session = Hosted Payment Page von Stripe
// → User wird auf Stripe Website redirected → einfacher!
// → Payment Intent = Für eigene Payment UI (komplexer)
func (s *Stripe) CreatePaymentLink(o *pb.Order) (string, error) {
	log.Printf("Creating payment link for order ID:%q customerID:%q Status:%q Items:%+v",
		o.Id, o.CustomerId, o.Status, o.Items)

	if o == nil {
		return "", fmt.Errorf("order is nil")
	}

	// Warum lineItems aus Order.Items bauen?
	// → Stripe braucht: Price ID + Quantity
	// → Order hat bereits: item.PriceID + item.Quantity
	// → 1:1 Mapping!
	var lineItems []*stripe.CheckoutSessionLineItemParams
	for _, item := range o.Items {
		lineItems = append(lineItems, &stripe.CheckoutSessionLineItemParams{
			Price:    stripe.String(item.PriceID),  // Stripe Price ID (z.B. "price_1PA7...")
			Quantity: stripe.Int64(int64(item.Quantity)),
		})
	}

	// Warum SuccessURL + CancelURL?
	// → SuccessURL: Wohin nach erfolgreicher Payment? → Customer App
	// → CancelURL: User klickt "Zurück" → Customer App
	// Dynamic: Use GATEWAY_PUBLIC_URL or GATEWAY_BASE_URL for test/dev/prod, fallback to localhost
	gatewayBaseURL := os.Getenv("GATEWAY_PUBLIC_URL")
	if gatewayBaseURL == "" {
		gatewayBaseURL = os.Getenv("GATEWAY_BASE_URL")
	}
	if gatewayBaseURL == "" {
		gatewayBaseURL = "http://localhost:8080"
	}
	gatewaySuccessURL := fmt.Sprintf("%s/success.html?customerID=%s&orderID=%s", gatewayBaseURL, o.CustomerId, o.Id)
	gatewayCancelURL := fmt.Sprintf("%s/cancel.html", gatewayBaseURL)

	// Warum Metadata?
	// → Stripe speichert orderID + customerID
	// → Bei Webhooks: Stripe sendet Metadata zurück!
	// → Wichtig für "Welche Order wurde bezahlt?"
	params := &stripe.CheckoutSessionParams{
		Metadata: map[string]string{
			"orderID":    o.Id,
			"customerID": o.CustomerId,
		},
		// PaymentMethodTypes: NICHT setzen! → Stripe zeigt automatisch ALLE aktivierten Methods!
		// Wenn PaymentMethodTypes weggelassen wird, zeigt Stripe Checkout automatisch:
		// → Card (Kredit/Debit)
		// → Apple Pay (auf Safari/iOS)
		// → Google Pay (auf Chrome/Android)
		// → PayPal (wenn aktiviert)
		// → Klarna (wenn aktiviert)
		// → SEPA, Bancontact, iDEAL, etc. (je nach Land/Währung)
		// Das ist das Verhalten das du vorher hattest!
		LineItems:          lineItems,
		Mode:               stripe.String(string(stripe.CheckoutSessionModePayment)), // "payment" (einmalig, nicht subscription)
		SuccessURL:         stripe.String(gatewaySuccessURL),
		CancelURL:          stripe.String(gatewayCancelURL),
	}

	// Warum session.New?
	// → Ruft Stripe API: POST /v1/checkout/sessions
	// → Gibt CheckoutSession zurück mit URL (z.B. "https://checkout.stripe.com/c/pay/cs_test_...")
	result, err := session.New(params)
	if err != nil {
		log.Printf("[ERROR] Request error from Stripe (status 400): %v", err)
		return "", fmt.Errorf("failed to create stripe session: %w", err)
	}

	log.Printf("Payment link created: %s", result.URL)
	return result.URL, nil  // URL: User kann auf diesen Link klicken!
}
