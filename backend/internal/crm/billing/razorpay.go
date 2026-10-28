package billing

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Razorpay (D-164): orders are made on the server with the secret key; the browser opens
// Razorpay Checkout for that order and hands back the payment id and signature, which the
// server checks before it counts the payment. The webhook is the backup for a browser that
// closes before it can report back.

type Config struct {
	KeyID         string // RAZORPAY_KEY_ID (public, sent to the browser)
	KeySecret     string // RAZORPAY_KEY_SECRET (server only)
	WebhookSecret string // RAZORPAY_WEBHOOK_SECRET (server only; empty = no webhook)
	APIBase       string // RAZORPAY_API_BASE, for tests (default https://api.razorpay.com)
}

func LoadConfig() Config {
	c := Config{
		KeyID:         strings.TrimSpace(os.Getenv("RAZORPAY_KEY_ID")),
		KeySecret:     strings.TrimSpace(os.Getenv("RAZORPAY_KEY_SECRET")),
		WebhookSecret: strings.TrimSpace(os.Getenv("RAZORPAY_WEBHOOK_SECRET")),
		APIBase:       strings.TrimRight(strings.TrimSpace(os.Getenv("RAZORPAY_API_BASE")), "/"),
	}
	if c.APIBase == "" {
		c.APIBase = "https://api.razorpay.com"
	}
	return c
}

// Ready: payments can be taken (both keys set).
func (c Config) Ready() bool { return c.KeyID != "" && c.KeySecret != "" }

type razorpayOrder struct {
	ID       string `json:"id"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
	Status   string `json:"status"`
}

func (c Config) createOrder(ctx context.Context, amountPaise int64, receipt string, notes map[string]string) (*razorpayOrder, error) {
	body, _ := json.Marshal(map[string]any{"amount": amountPaise, "currency": "INR", "receipt": receipt, "notes": notes})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.APIBase+"/v1/orders", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.KeyID, c.KeySecret)
	req.Header.Set("Content-Type", "application/json")
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Description string `json:"description"`
			} `json:"error"`
		}
		_ = json.Unmarshal(raw, &e)
		return nil, fmt.Errorf("razorpay order: %d %s", res.StatusCode, e.Error.Description)
	}
	var o razorpayOrder
	if err := json.Unmarshal(raw, &o); err != nil || o.ID == "" {
		return nil, fmt.Errorf("razorpay order: unreadable answer")
	}
	return &o, nil
}

func hmacHex(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

// paymentSignatureOK checks Checkout's signature: HMAC-SHA256(order_id|payment_id, key secret).
func (c Config) paymentSignatureOK(orderID, paymentID, signature string) bool {
	if c.KeySecret == "" || orderID == "" || paymentID == "" || signature == "" {
		return false
	}
	return hmac.Equal([]byte(hmacHex(c.KeySecret, orderID+"|"+paymentID)), []byte(strings.ToLower(signature)))
}

// webhookSignatureOK checks X-Razorpay-Signature: HMAC-SHA256(raw body, webhook secret).
func (c Config) webhookSignatureOK(body []byte, signature string) bool {
	if c.WebhookSecret == "" || signature == "" {
		return false
	}
	return hmac.Equal([]byte(hmacHex(c.WebhookSecret, string(body))), []byte(strings.ToLower(signature)))
}

func readAll(r *http.Request, max int64) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r.Body, max))
}

// UseConfig swaps the Razorpay settings (tests point it at a fake Razorpay).
func (s *Service) UseConfig(c Config) {
	s.mu.Lock()
	s.cfg = c
	s.mu.Unlock()
}
