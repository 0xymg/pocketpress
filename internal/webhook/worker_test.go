package webhook

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pocketpress/pocketpress/internal/store"
)

// capturedReq is one request a test receiver saw, with the signature already
// re-verified against the known secret.
type capturedReq struct {
	event       string
	delivery    string
	timestamp   string
	userAgent   string
	contentType string
	body        []byte
	sigOK       bool
}

// newReceiver starts a fake webhook receiver that records every request (and
// whether its HMAC signature verifies against secret) and answers with the
// given status and response body.
func newReceiver(t *testing.T, secret string, status int, respBody string) (url string, requests func() []capturedReq) {
	t.Helper()
	var mu sync.Mutex
	var reqs []capturedReq
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		ts := r.Header.Get(headerTimestamp)
		tsInt, _ := strconv.ParseInt(ts, 10, 64)
		expected := Sign([]byte(secret), tsInt, body)
		cr := capturedReq{
			event:       r.Header.Get(headerEvent),
			delivery:    r.Header.Get(headerDelivery),
			timestamp:   ts,
			userAgent:   r.Header.Get("User-Agent"),
			contentType: r.Header.Get("Content-Type"),
			body:        body,
			sigOK:       r.Header.Get(headerSignature) == expected,
		}
		mu.Lock()
		reqs = append(reqs, cr)
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, respBody)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, func() []capturedReq {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedReq(nil), reqs...)
	}
}

// TestWorkerDeliversAndSignsSuccess confirms a 2xx delivery is marked success
// with the response recorded, and that the receiver could verify the signature.
func TestWorkerDeliversAndSignsSuccess(t *testing.T) {
	db := newTestDB(t)
	cipher := testCipher()
	dp := NewDispatcher(db, nil)
	svc := NewService(db, cipher, dp, false, nil)

	url, requests := newReceiver(t, "", http.StatusOK, "ok") // secret filled after create
	wh, secret, err := svc.Create(context.Background(), CreateInput{
		Name: "ok-hook", URL: url, Events: []string{EventPostPublished}, Active: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	// Rebuild the receiver now that we know the secret (so it can verify sigs).
	url, requests = newReceiver(t, secret, http.StatusOK, "ok")
	if _, err := svc.Update(context.Background(), wh.ID, UpdatePatch{URL: &url}); err != nil {
		t.Fatalf("Update(url) error = %v", err)
	}

	deliveryID, err := dp.EnqueueTest(context.Background(), wh.ID)
	if err != nil {
		t.Fatalf("EnqueueTest() error = %v", err)
	}

	w := NewWorker(db, cipher, &http.Client{}, nil)
	w.processDue(context.Background())

	d := getDelivery(t, db, deliveryID)
	if d.Status != store.DeliveryStatusSuccess {
		t.Fatalf("status = %q, want success", d.Status)
	}
	if d.DeliveredAt == nil {
		t.Fatal("delivered_at = nil, want set")
	}
	if d.ResponseCode == nil || *d.ResponseCode != http.StatusOK {
		t.Fatalf("response_code = %v, want 200", d.ResponseCode)
	}
	if d.ResponseBody == nil || *d.ResponseBody != "ok" {
		t.Fatalf("response_body = %v, want \"ok\"", d.ResponseBody)
	}
	if d.Attempt != 1 {
		t.Fatalf("attempt = %d, want 1", d.Attempt)
	}

	reqs := requests()
	if len(reqs) != 1 {
		t.Fatalf("receiver saw %d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if !r.sigOK {
		t.Fatal("receiver could not verify the HMAC signature")
	}
	if r.event != EventTest || r.delivery != d.EventID {
		t.Fatalf("headers event=%q delivery=%q, want webhook.test / %q", r.event, r.delivery, d.EventID)
	}
	if r.userAgent != defaultUserAgent {
		t.Fatalf("User-Agent = %q, want %q", r.userAgent, defaultUserAgent)
	}
	if !strings.HasPrefix(r.contentType, "application/json") {
		t.Fatalf("Content-Type = %q, want application/json", r.contentType)
	}
}

// TestWorkerRetryScheduleProgression drives a persistently-failing (500)
// delivery through the retry schedule with an injected clock: each failed
// attempt bumps the attempt counter and re-arms next_attempt_at, until the 5th
// failure marks the delivery failed. Every attempt is signed with a fresh
// timestamp the receiver verifies.
func TestWorkerRetryScheduleProgression(t *testing.T) {
	db := newTestDB(t)
	cipher := testCipher()
	dp := NewDispatcher(db, nil)
	svc := NewService(db, cipher, dp, false, nil)

	// Placeholder create, then rebuild receiver with the known secret.
	wh, secret, err := svc.Create(context.Background(), CreateInput{
		Name: "fail-hook", URL: "https://placeholder.example.com", Events: nil, Active: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	url, requests := newReceiver(t, secret, http.StatusInternalServerError, "boom")
	if _, err := svc.Update(context.Background(), wh.ID, UpdatePatch{URL: &url}); err != nil {
		t.Fatalf("Update(url) error = %v", err)
	}

	deliveryID, err := dp.EnqueueTest(context.Background(), wh.ID)
	if err != nil {
		t.Fatalf("EnqueueTest() error = %v", err)
	}

	w := NewWorker(db, cipher, &http.Client{}, nil)
	// Whole-second clock: stored next_attempt_at is RFC3339 (second precision),
	// so a truncated clock compares cleanly.
	fakeNow := time.Now().UTC().Truncate(time.Second)
	w.now = func() time.Time { return fakeNow }
	retryDelay := time.Minute
	w.schedule = []time.Duration{0, retryDelay, retryDelay, retryDelay, retryDelay}

	for attempt := 1; attempt <= 5; attempt++ {
		w.processDue(context.Background())
		d := getDelivery(t, db, deliveryID)
		if d.Attempt != attempt {
			t.Fatalf("after pass %d attempt = %d, want %d", attempt, d.Attempt, attempt)
		}
		if attempt < 5 {
			if d.Status != store.DeliveryStatusPending {
				t.Fatalf("after pass %d status = %q, want pending", attempt, d.Status)
			}
			wantNext := fakeNow.Add(retryDelay)
			if d.NextAttemptAt == nil || !d.NextAttemptAt.Equal(wantNext) {
				t.Fatalf("after pass %d next_attempt_at = %v, want %v", attempt, d.NextAttemptAt, wantNext)
			}
		} else {
			if d.Status != store.DeliveryStatusFailed {
				t.Fatalf("after final pass status = %q, want failed", d.Status)
			}
			if d.NextAttemptAt != nil {
				t.Fatalf("failed delivery next_attempt_at = %v, want nil", d.NextAttemptAt)
			}
		}
		// Advance the clock past the just-scheduled next attempt so the next
		// pass sees the row as due.
		fakeNow = fakeNow.Add(2 * retryDelay)
	}

	reqs := requests()
	if len(reqs) != 5 {
		t.Fatalf("receiver saw %d requests, want 5 attempts", len(reqs))
	}
	seenTs := map[string]bool{}
	for i, r := range reqs {
		if !r.sigOK {
			t.Fatalf("attempt %d signature did not verify", i+1)
		}
		if seenTs[r.timestamp] {
			t.Fatalf("attempt %d reused timestamp %q; want a fresh timestamp per attempt", i+1, r.timestamp)
		}
		seenTs[r.timestamp] = true
	}
}

// TestWorkerManualRetry confirms a manually re-queued failed delivery gets one
// more attempt (attempt counter continuing) and, on another failure, returns to
// failed.
func TestWorkerManualRetry(t *testing.T) {
	db := newTestDB(t)
	cipher := testCipher()
	dp := NewDispatcher(db, nil)
	svc := NewService(db, cipher, dp, false, nil)

	wh, secret, err := svc.Create(context.Background(), CreateInput{
		Name: "retry-hook", URL: "https://placeholder.example.com", Events: nil, Active: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	url, _ := newReceiver(t, secret, http.StatusInternalServerError, "boom")
	if _, err := svc.Update(context.Background(), wh.ID, UpdatePatch{URL: &url}); err != nil {
		t.Fatalf("Update(url) error = %v", err)
	}

	deliveryID, err := dp.EnqueueTest(context.Background(), wh.ID)
	if err != nil {
		t.Fatalf("EnqueueTest() error = %v", err)
	}

	w := NewWorker(db, cipher, &http.Client{}, nil)
	// A fixed future clock with a zero-delay schedule: every pass keeps the row
	// due, so five passes exhaust the attempts without advancing time.
	fakeNow := time.Now().UTC().Add(time.Hour)
	w.now = func() time.Time { return fakeNow }
	w.schedule = []time.Duration{0, 0, 0, 0, 0}

	for i := 0; i < 5; i++ {
		w.processDue(context.Background())
	}
	if d := getDelivery(t, db, deliveryID); d.Status != store.DeliveryStatusFailed || d.Attempt != 5 {
		t.Fatalf("after 5 passes = {status:%q attempt:%d}, want {failed 5}", d.Status, d.Attempt)
	}

	// Manual retry re-queues it; the attempt counter continues from 5.
	if err := svc.RetryDelivery(context.Background(), deliveryID); err != nil {
		t.Fatalf("RetryDelivery() error = %v", err)
	}
	if d := getDelivery(t, db, deliveryID); d.Status != store.DeliveryStatusPending {
		t.Fatalf("after retry status = %q, want pending", d.Status)
	}

	// One more failing attempt returns it to failed at attempt 6.
	w.processDue(context.Background())
	if d := getDelivery(t, db, deliveryID); d.Status != store.DeliveryStatusFailed || d.Attempt != 6 {
		t.Fatalf("after retry pass = {status:%q attempt:%d}, want {failed 6}", d.Status, d.Attempt)
	}

	// Retrying a non-failed (already-pending) delivery is rejected. Re-queue,
	// then attempt to retry while pending.
	if err := svc.RetryDelivery(context.Background(), deliveryID); err != nil {
		t.Fatalf("second RetryDelivery() error = %v", err)
	}
	if err := svc.RetryDelivery(context.Background(), deliveryID); err != ErrDeliveryNotFailed {
		t.Fatalf("retry of pending delivery error = %v, want ErrDeliveryNotFailed", err)
	}
}

// TestWorkerTruncatesResponseBody confirms a large response body is truncated
// to at most maxResponseBodyBytes before storage.
func TestWorkerTruncatesResponseBody(t *testing.T) {
	db := newTestDB(t)
	cipher := testCipher()
	dp := NewDispatcher(db, nil)
	svc := NewService(db, cipher, dp, false, nil)

	wh, secret, err := svc.Create(context.Background(), CreateInput{
		Name: "big-hook", URL: "https://placeholder.example.com", Events: nil, Active: true,
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	big := strings.Repeat("A", 5000)
	url, _ := newReceiver(t, secret, http.StatusOK, big)
	if _, err := svc.Update(context.Background(), wh.ID, UpdatePatch{URL: &url}); err != nil {
		t.Fatalf("Update(url) error = %v", err)
	}

	deliveryID, err := dp.EnqueueTest(context.Background(), wh.ID)
	if err != nil {
		t.Fatalf("EnqueueTest() error = %v", err)
	}

	w := NewWorker(db, cipher, &http.Client{}, nil)
	w.processDue(context.Background())

	d := getDelivery(t, db, deliveryID)
	if d.ResponseBody == nil {
		t.Fatal("response_body = nil, want the truncated body")
	}
	if len(*d.ResponseBody) > maxResponseBodyBytes {
		t.Fatalf("response_body length = %d, want <= %d", len(*d.ResponseBody), maxResponseBodyBytes)
	}
}
