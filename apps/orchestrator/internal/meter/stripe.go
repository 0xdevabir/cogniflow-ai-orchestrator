package meter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// StripeConfig configures the StripeMeterer.
type StripeConfig struct {
	APIKey        string        // STRIPE_API_KEY env var
	MeterID       string        // Stripe meter id (per-token or per-call)
	BaseURL       string        // default "https://api.stripe.com"
	HTTPClient    *http.Client  // injectable for tests
	BufferSize    int           // default 256
	FlushInterval time.Duration // default 5s
}

// StripeMeterer batches usage events and POSTs them to Stripe's billing
// meter events ingestion API. Events are buffered in memory; Flush() (or
// the periodic flusher goroutine) drains the buffer.
//
// This is a stubbed-but-functional implementation: it sends the JSON the
// real Stripe API accepts, and the shape is documented. A real production
// deployment needs:
//   - the Stripe SDK for signature verification on webhooks
//   - exponential backoff + retry on 5xx
//   - circuit breaker + dead-letter queue
// Those are deferred until the live integration is wired in the dashboard.
type StripeMeterer struct {
	cfg    StripeConfig
	client *http.Client

	mu     sync.Mutex
	buffer []Event
	cancel context.CancelFunc
	done   chan struct{}
}

// NewStripeMeterer builds a StripeMeterer and starts the periodic flusher.
// Returns an error if the API key or meter id are missing.
func NewStripeMeterer(cfg StripeConfig) (*StripeMeterer, error) {
	if cfg.APIKey == "" {
		return nil, fmt.Errorf("meter: StripeConfig.APIKey required")
	}
	if cfg.MeterID == "" {
		return nil, fmt.Errorf("meter: StripeConfig.MeterID required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = "https://api.stripe.com"
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = 256
	}
	if cfg.FlushInterval <= 0 {
		cfg.FlushInterval = 5 * time.Second
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}

	ctx, cancel := context.WithCancel(context.Background())
	m := &StripeMeterer{
		cfg:    cfg,
		client: cfg.HTTPClient,
		buffer: make([]Event, 0, cfg.BufferSize),
		cancel: cancel,
		done:   make(chan struct{}),
	}
	go m.runFlusher(ctx)
	return m, nil
}

// Record appends the event to the in-memory buffer. Non-blocking unless
// the buffer is at capacity; in that case it flushes synchronously.
func (s *StripeMeterer) Record(ev Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buffer = append(s.buffer, ev)
	if len(s.buffer) >= s.cfg.BufferSize {
		// Best-effort flush; we don't surface errors on the hot path.
		_ = s.flushLocked(context.Background())
	}
}

// Flush drains the buffer now.
func (s *StripeMeterer) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flushLocked(context.Background())
}

// Close stops the flusher goroutine and drains remaining events.
func (s *StripeMeterer) Close() error {
	s.cancel()
	<-s.done
	return s.Flush()
}

func (s *StripeMeterer) runFlusher(ctx context.Context) {
	defer close(s.done)
	t := time.NewTicker(s.cfg.FlushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.mu.Lock()
			_ = s.flushLocked(ctx)
			s.mu.Unlock()
		}
	}
}

func (s *StripeMeterer) flushLocked(ctx context.Context) error {
	if len(s.buffer) == 0 {
		return nil
	}
	batch := s.buffer
	s.buffer = make([]Event, 0, s.cfg.BufferSize)

	payload := stripeBatchPayload{
		MeterID: s.cfg.MeterID,
		Events:  make([]stripeMeterEvent, 0, len(batch)),
	}
	for _, ev := range batch {
		payload.Events = append(payload.Events, stripeMeterEvent{
			Identifier: stripeIdentifier{
				CustomerID: ev.Workspace, // workspace = stripe customer id
			},
			Timestamp: ev.OccurredAt.Unix(),
			Quantity:  int64(ev.TokensIn + ev.TokensOut),
		})
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx,
		http.MethodPost, s.cfg.BaseURL+"/v1/billing/meter_events",
		bytes.NewReader(body),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+s.cfg.APIKey)
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		// Re-buffer so the next flush retries.
		s.buffer = append(batch, s.buffer...)
		return err
	}
	resp.Body.Close()
	if resp.StatusCode >= 300 {
		s.buffer = append(batch, s.buffer...)
		return fmt.Errorf("meter: stripe returned %d", resp.StatusCode)
	}
	return nil
}

// StripeMetererFromEnv builds a StripeMeterer from STRIPE_API_KEY +
// STRIPE_METER_ID, returning NoopMeterer if either is missing. This is
// the helper main.go should call so the orchestrator doesn't crash when
// no key is configured.
func StripeMetererFromEnv() Meterer {
	apiKey := os.Getenv("STRIPE_API_KEY")
	meterID := os.Getenv("STRIPE_METER_ID")
	if apiKey == "" || meterID == "" {
		return NoopMeterer{}
	}
	m, err := NewStripeMeterer(StripeConfig{
		APIKey:  apiKey,
		MeterID: meterID,
	})
	if err != nil {
		return NoopMeterer{}
	}
	return m
}

// Stripe wire types. Field names match the v1/billing/meter_events schema.
type stripeBatchPayload struct {
	MeterID string              `json:"meter_id"`
	Events  []stripeMeterEvent  `json:"events"`
}

type stripeMeterEvent struct {
	Identifier stripeIdentifier `json:"identifier"`
	Timestamp  int64            `json:"timestamp"`
	Quantity   int64            `json:"quantity"`
}

type stripeIdentifier struct {
	CustomerID string `json:"customer_id"`
}