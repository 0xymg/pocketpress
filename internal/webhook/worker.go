package webhook

import (
	"bytes"
	"context"
	"database/sql"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// Delivery request constants (development plan §8).
const (
	// defaultUserAgent is the User-Agent sent when settings.webhook_user_agent
	// is blank.
	defaultUserAgent = "PocketPress/0.1.0"

	// deliveryTimeout bounds a single delivery HTTP request.
	deliveryTimeout = 10 * time.Second

	// maxResponseBodyBytes caps how much of a receiver's response body is
	// stored (truncated to ~1 KB).
	maxResponseBodyBytes = 1024

	// workerInterval is how often the worker scans for due deliveries.
	workerInterval = 5 * time.Second

	// deliveryBatch caps how many due deliveries one pass processes, so a
	// backlog is drained in bounded, ordered chunks.
	deliveryBatch = 50

	headerEvent     = "X-PocketPress-Event"
	headerDelivery  = "X-PocketPress-Delivery"
	headerTimestamp = "X-PocketPress-Timestamp"
	headerSignature = "X-PocketPress-Signature"
)

// DefaultSchedule is the frozen retry schedule (development plan §8), indexed
// by attempt number: index 0 is the immediate first try, then +1m, +5m, +30m,
// +2h before attempts 2..5. Its length (5) is also the maximum number of
// attempts — after the 5th failed attempt a delivery is marked failed.
func DefaultSchedule() []time.Duration {
	return []time.Duration{
		0,
		1 * time.Minute,
		5 * time.Minute,
		30 * time.Minute,
		2 * time.Hour,
	}
}

// Worker drains the DB-backed delivery queue: every workerInterval it claims
// pending deliveries that are due, POSTs each with its per-attempt HMAC
// signature, and records the outcome — advancing the retry schedule on failure
// or marking the delivery failed after the last attempt.
type Worker struct {
	db       *sql.DB
	cipher   *Cipher
	client   *http.Client
	logger   *slog.Logger
	interval time.Duration
	schedule []time.Duration
	now      func() time.Time
}

// NewDeliveryClient builds the HTTP client the delivery Worker uses: a
// 10s-timeout client whose transport carries the production SSRF dial guard (in
// production) or a plain dialer (in development, so local receivers work).
func NewDeliveryClient(production bool) *http.Client {
	return &http.Client{Transport: newDeliveryTransport(production, nil), Timeout: deliveryTimeout}
}

// NewWorker builds a Worker. client is the delivery HTTP client (its transport
// carries the production SSRF dial guard, if any); its per-request timeout is
// set here. logger may be nil.
func NewWorker(db *sql.DB, cipher *Cipher, client *http.Client, logger *slog.Logger) *Worker {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	if client == nil {
		client = &http.Client{}
	}
	client.Timeout = deliveryTimeout
	return &Worker{
		db:       db,
		cipher:   cipher,
		client:   client,
		logger:   logger,
		interval: workerInterval,
		schedule: DefaultSchedule(),
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// Run drains due deliveries once immediately (so a backlog left by a restart
// starts clearing at once), then every interval, until ctx is cancelled. It is
// meant to run in its own goroutine for the server's lifetime.
func (w *Worker) Run(ctx context.Context) {
	w.processDue(ctx)

	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			w.processDue(ctx)
		}
	}
}

// processDue delivers every currently-due pending delivery, sequentially.
func (w *Worker) processDue(ctx context.Context) {
	due, err := store.ListDueDeliveries(ctx, w.db, w.now(), deliveryBatch)
	if err != nil {
		w.logger.Error("webhook worker: list due deliveries failed", slog.String("error", err.Error()))
		return
	}
	if len(due) == 0 {
		return
	}
	userAgent := w.userAgent(ctx)
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		w.deliverOne(ctx, d, userAgent)
	}
}

// deliverOne performs one delivery attempt and records its result.
func (w *Worker) deliverOne(ctx context.Context, d store.Delivery, userAgent string) {
	wh, err := store.GetWebhookByID(ctx, w.db, d.WebhookID)
	if err != nil {
		// The webhook row is gone (its deliveries should have cascaded, but be
		// defensive): record a terminal failure rather than looping forever.
		w.logger.Warn("webhook worker: delivery references missing webhook",
			slog.String("delivery_id", d.ID), slog.String("webhook_id", d.WebhookID))
		w.record(ctx, d, resultFor(w, d, false, nil, ptr("webhook not found")))
		return
	}

	secret, err := w.cipher.Decrypt(wh.SecretEncrypted)
	if err != nil {
		w.logger.Error("webhook worker: decrypt secret failed",
			slog.String("delivery_id", d.ID), slog.String("error", err.Error()))
		w.record(ctx, d, resultFor(w, d, false, nil, ptr("secret decrypt failed")))
		return
	}

	body := []byte(d.Payload)
	timestamp := w.now().Unix()
	signature := Sign([]byte(secret), timestamp, body)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, bytes.NewReader(body))
	if err != nil {
		w.record(ctx, d, resultFor(w, d, false, nil, ptr(truncate(err.Error()))))
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set(headerEvent, d.Event)
	req.Header.Set(headerDelivery, d.EventID)
	req.Header.Set(headerTimestamp, strconv.FormatInt(timestamp, 10))
	req.Header.Set(headerSignature, signature)

	resp, err := w.client.Do(req)
	if err != nil {
		// Transport-level failure (connection refused, timeout, blocked private
		// address): no HTTP status, error text recorded as the response body.
		w.record(ctx, d, resultFor(w, d, false, nil, ptr(truncate(err.Error()))))
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodyBytes))
	code := resp.StatusCode
	success := code >= 200 && code < 300
	bodyStr := truncate(string(respBody))
	w.record(ctx, d, resultFor(w, d, success, &code, &bodyStr))
}

// resultFor computes the stored outcome of an attempt on d given whether it
// succeeded and the response code/body: success stamps delivered_at; a failure
// advances the retry schedule, or marks the delivery failed once every attempt
// is spent.
func resultFor(w *Worker, d store.Delivery, success bool, code *int, respBody *string) store.DeliveryResult {
	attempt := d.Attempt + 1
	now := w.now().UTC()

	if success {
		delivered := now
		return store.DeliveryResult{
			Status:       store.DeliveryStatusSuccess,
			Attempt:      attempt,
			ResponseCode: code,
			ResponseBody: respBody,
			DeliveredAt:  &delivered,
		}
	}
	if attempt >= len(w.schedule) {
		return store.DeliveryResult{
			Status:       store.DeliveryStatusFailed,
			Attempt:      attempt,
			ResponseCode: code,
			ResponseBody: respBody,
		}
	}
	next := now.Add(w.schedule[attempt])
	return store.DeliveryResult{
		Status:        store.DeliveryStatusPending,
		Attempt:       attempt,
		ResponseCode:  code,
		ResponseBody:  respBody,
		NextAttemptAt: &next,
	}
}

func (w *Worker) record(ctx context.Context, d store.Delivery, r store.DeliveryResult) {
	if err := store.UpdateDeliveryResult(ctx, w.db, d.ID, r); err != nil {
		w.logger.Error("webhook worker: record delivery result failed",
			slog.String("delivery_id", d.ID), slog.String("error", err.Error()))
	}
}

// userAgent returns the configured settings.webhook_user_agent, or the default
// when it is unset/blank or settings are not yet created.
func (w *Worker) userAgent(ctx context.Context) string {
	s, err := store.GetSettings(ctx, w.db)
	if err != nil || strings.TrimSpace(s.WebhookUserAgent) == "" {
		return defaultUserAgent
	}
	return s.WebhookUserAgent
}

// truncate clips s to at most maxResponseBodyBytes bytes, dropping any trailing
// partial UTF-8 rune so the stored TEXT stays valid.
func truncate(s string) string {
	if len(s) <= maxResponseBodyBytes {
		return s
	}
	return strings.ToValidUTF8(s[:maxResponseBodyBytes], "")
}

func ptr[T any](v T) *T { return &v }
