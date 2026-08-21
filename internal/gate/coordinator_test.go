package gate

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
)

type countingNotifier struct {
	mu   sync.Mutex
	sent []notify.Notification
}

func (notifier *countingNotifier) Send(_ context.Context, notification notify.Notification) error {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	notifier.sent = append(notifier.sent, notification)
	return nil
}

func (notifier *countingNotifier) count(summary string) int {
	notifier.mu.Lock()
	defer notifier.mu.Unlock()
	total := 0
	for _, notification := range notifier.sent {
		if notification.Summary == summary {
			total++
		}
	}
	return total
}

func newTestCoordinator(timeout time.Duration, notifier notify.Notifier) *Coordinator {
	return NewCoordinator(timeout, time.Minute, notifier, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func testRequest(fingerprint string) Request {
	return Request{
		KeyFormat:      "ssh-ed25519",
		KeyFingerprint: fingerprint,
		KeyComment:     "test-key",
		ClientPID:      4242,
		ClientExe:      "/usr/bin/ssh",
		ClientCmdline:  "ssh git@github.com",
		ParentChain:    "ssh <- git <- jj",
	}
}

func requestWithMemoryKey(fingerprint string, memoryKey string) Request {
	request := testRequest(fingerprint)
	request.MemoryKey = memoryKey
	return request
}

func TestSubmitDeniedWithoutController(t *testing.T) {
	notifier := &countingNotifier{}
	coordinator := newTestCoordinator(100*time.Millisecond, notifier)

	result, err := coordinator.Submit(context.Background(), testRequest("SHA256:aaa"))

	if result.Outcome != OutcomeDenied || result.Source != SourceTimeout {
		t.Fatalf("Submit() result = %+v, want denied/timeout", result)
	}
	if err == nil {
		t.Fatal("Submit() error = nil, want error")
	}
	if got := notifier.count("SSH signature denied"); got != 1 {
		t.Fatalf("denied notifications = %d, want 1", got)
	}
	if got := notifier.count("SSH signature request"); got != 1 {
		t.Fatalf("pending notifications = %d, want 0", got)
	}
}

func TestSubmitControllerDecision(t *testing.T) {
	for _, tc := range []struct {
		name    string
		allow   bool
		outcome Outcome
	}{
		{"allow", true, OutcomeAllowed},
		{"deny", false, OutcomeDenied},
	} {
		t.Run(tc.name, func(t *testing.T) {
			coordinator := newTestCoordinator(100*time.Millisecond, notify.Noop())
			coordinator.ControllerConnected()
			events, cancel := coordinator.Subscribe()
			defer cancel()

			results := make(chan Result, 1)
			go func() {
				result, err := coordinator.Submit(context.Background(), testRequest("SHA256:bbb"))
				if (err == nil) != (result.Outcome == OutcomeAllowed) {
					t.Errorf("Submit() = (%+v, %v), inconsistent outcome and error", result, err)
				}
				results <- result
			}()

			event := nextEvent(t, events, EventPending)
			if event.Request.KeyComment != "test-key" {
				t.Fatalf("pending event comment = %q, want test-key", event.Request.KeyComment)
			}

			if err := coordinator.Decide(event.Request.ID, tc.allow, 0); err != nil {
				t.Fatalf("Decide() error = %v", err)
			}

			result := <-results
			if result.Outcome != tc.outcome || result.Source != SourceController {
				t.Fatalf("Submit() result = %+v, want %s/controller", result, tc.outcome)
			}

			resolved := nextEvent(t, events, EventResolved)
			if resolved.ID != event.Request.ID || resolved.Result.Outcome != tc.outcome {
				t.Fatalf("resolved event = %+v, want id %q outcome %s", resolved, event.Request.ID, tc.outcome)
			}
		})
	}
}

func TestSubmitTimeout(t *testing.T) {
	coordinator := newTestCoordinator(30*time.Millisecond, notify.Noop())
	coordinator.ControllerConnected()

	start := time.Now()
	result, err := coordinator.Submit(context.Background(), testRequest("SHA256:ccc"))
	elapsed := time.Since(start)

	if result.Outcome != OutcomeDenied || result.Source != SourceTimeout {
		t.Fatalf("Submit() result = %+v, want denied/timeout", result)
	}
	if err == nil {
		t.Fatal("Submit() error = nil, want error")
	}
	if elapsed < 25*time.Millisecond {
		t.Fatalf("Submit() returned after %s, want a timeout wait", elapsed)
	}
}

func TestSubmitShutdownContext(t *testing.T) {
	coordinator := newTestCoordinator(100*time.Millisecond, notify.Noop())
	coordinator.ControllerConnected()
	ctx, cancel := context.WithCancel(context.Background())

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(ctx, testRequest("SHA256:ddd"))
		results <- result
	}()

	events, cancelEvents := coordinator.Subscribe()
	defer cancelEvents()
	nextEvent(t, events, EventPending)
	cancel()

	result := <-results
	if result.Outcome != OutcomeDenied || result.Source != SourceShutdown {
		t.Fatalf("Submit() result = %+v, want denied/shutdown", result)
	}
}

func TestDecideUnknownRequest(t *testing.T) {
	coordinator := newTestCoordinator(100*time.Millisecond, notify.Noop())
	coordinator.ControllerConnected()

	if err := coordinator.Decide("404", true, 0); err == nil {
		t.Fatal("Decide() error = nil, want error for unknown id")
	}
}

func TestSubmitAllowedByRememberedDecision(t *testing.T) {
	notifier := &countingNotifier{}
	coordinator := newTestCoordinator(100*time.Millisecond, notifier)
	coordinator.ControllerConnected()
	events, cancel := coordinator.Subscribe()
	defer cancel()

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:mem", "mem-A"))
		results <- result
	}()
	event := nextEvent(t, events, EventPending)
	if err := coordinator.Decide(event.Request.ID, true, time.Minute); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result := <-results; result.Source != SourceController {
		t.Fatalf("first Submit() result = %+v, want controller decision", result)
	}

	replay, err := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:mem", "mem-A"))

	if replay.Outcome != OutcomeAllowed || replay.Source != SourceMemory {
		t.Fatalf("replay Submit() = (%+v, %v), want allowed/memory", replay, err)
	}
	if got := notifier.count("SSH signature request"); got != 1 {
		t.Fatalf("pending notifications = %d, want 1 (replay must be silent)", got)
	}
}

func TestRememberedDecisionExpires(t *testing.T) {
	coordinator := NewCoordinator(80*time.Millisecond, time.Minute, notify.Noop(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	coordinator.ControllerConnected()
	events, cancel := coordinator.Subscribe()
	defer cancel()

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:exp", "mem-B"))
		results <- result
	}()
	event := nextEvent(t, events, EventPending)
	if err := coordinator.Decide(event.Request.ID, true, 40*time.Millisecond); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result := <-results; result.Source != SourceController {
		t.Fatalf("first Submit() result = %+v, want controller decision", result)
	}

	time.Sleep(60 * time.Millisecond)
	replay, err := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:exp", "mem-B"))

	if replay.Outcome != OutcomeDenied || replay.Source != SourceTimeout {
		t.Fatalf("expired replay Submit() = (%+v, %v), want denied/timeout (re-asked)", replay, err)
	}
}

func TestRememberDisabledIgnoresControllerRemember(t *testing.T) {
	coordinator := NewCoordinator(100*time.Millisecond, 0, notify.Noop(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	coordinator.ControllerConnected()
	events, cancel := coordinator.Subscribe()
	defer cancel()

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:off", "mem-C"))
		results <- result
	}()
	event := nextEvent(t, events, EventPending)
	if err := coordinator.Decide(event.Request.ID, true, time.Hour); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	<-results
	coordinator.ControllerDisconnected()

	replay, err := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:off", "mem-C"))

	if replay.Outcome != OutcomeDenied || replay.Source != SourceTimeout {
		t.Fatalf("replay Submit() = (%+v, %v), want timeout not silent allow", replay, err)
	}
}

func TestDecideDenyDoesNotRemember(t *testing.T) {
	coordinator := newTestCoordinator(100*time.Millisecond, notify.Noop())
	coordinator.ControllerConnected()
	events, cancel := coordinator.Subscribe()
	defer cancel()

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:dny", "mem-D"))
		results <- result
	}()
	event := nextEvent(t, events, EventPending)
	if err := coordinator.Decide(event.Request.ID, false, time.Hour); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}
	if result := <-results; result.Outcome != OutcomeDenied {
		t.Fatalf("first Submit() result = %+v, want denied", result)
	}
	coordinator.ControllerDisconnected()

	replay, _ := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:dny", "mem-D"))

	if replay.Outcome != OutcomeDenied || replay.Source != SourceTimeout {
		t.Fatalf("replay Submit() = %+v, want timeout after deny", replay)
	}
}

func TestDecideBatchResolvesSameMemoryKey(t *testing.T) {
	coordinator := newTestCoordinator(100*time.Millisecond, notify.Noop())
	coordinator.ControllerConnected()
	events, cancel := coordinator.Subscribe()
	defer cancel()

	results := make(chan Result, 2)
	for _, fingerprint := range []string{"SHA256:b1", "SHA256:b2"} {
		go func(fingerprint string) {
			result, _ := coordinator.Submit(t.Context(), requestWithMemoryKey(fingerprint, "mem-E"))
			results <- result
		}(fingerprint)
	}

	pending := make([]Event, 0, 2)
	waitFor(t, func() bool {
		for {
			select {
			case event := <-events:
				if event.Kind == EventPending {
					pending = append(pending, event)
				}
			default:
				return len(pending) == 2
			}
		}
	})
	if err := coordinator.Decide(pending[0].Request.ID, true, 0); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}

	for range 2 {
		if result := <-results; result.Outcome != OutcomeAllowed || result.Source != SourceController {
			t.Fatalf("batched Submit() result = %+v, want allowed/controller", result)
		}
	}
	if err := coordinator.Decide(pending[1].Request.ID, true, 0); err == nil {
		t.Fatal("late Decide() error = nil, want already-resolved error")
	}
}

func TestDecideTwiceRejects(t *testing.T) {
	coordinator := newTestCoordinator(100*time.Millisecond, notify.Noop())
	coordinator.ControllerConnected()
	events, cancel := coordinator.Subscribe()
	defer cancel()

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(context.Background(), testRequest("SHA256:eee"))
		results <- result
	}()

	event := nextEvent(t, events, EventPending)
	if err := coordinator.Decide(event.Request.ID, true, 0); err != nil {
		t.Fatalf("first Decide() error = %v", err)
	}
	if err := coordinator.Decide(event.Request.ID, true, 0); err == nil {
		t.Fatal("second Decide() error = nil, want already-resolved error")
	}
	<-results
}

func TestDoorbellRingsOncePerFingerprint(t *testing.T) {
	notifier := &countingNotifier{}
	coordinator := newTestCoordinator(100*time.Millisecond, notifier)
	coordinator.ControllerConnected()
	events, cancel := coordinator.Subscribe()
	defer cancel()

	results := make(chan Result, 2)
	for range 2 {
		go func() {
			result, _ := coordinator.Submit(context.Background(), testRequest("SHA256:fff"))
			results <- result
		}()
	}

	pending := make([]Event, 0, 2)
	waitFor(t, func() bool {
		for {
			select {
			case event := <-events:
				if event.Kind == EventPending {
					pending = append(pending, event)
				}
			default:
				return len(pending) == 2
			}
		}
	})
	for _, event := range pending {
		if err := coordinator.Decide(event.Request.ID, true, 0); err != nil {
			t.Fatalf("Decide(%q) error = %v", event.Request.ID, err)
		}
	}
	<-results
	<-results

	if got := notifier.count("SSH signature request"); got != 1 {
		t.Fatalf("pending notifications = %d, want 1 for duplicate fingerprint", got)
	}
}

func nextEvent(t *testing.T, events <-chan Event, kind EventKind) Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case event := <-events:
			if event.Kind == kind {
				return event
			}
		case <-time.After(time.Until(deadline)):
			t.Fatalf("event %s never arrived", kind)
			return Event{}
		}
	}
}

func waitFor(t *testing.T, condition func() bool) bool {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return true
		}
		time.Sleep(2 * time.Millisecond)
	}
	return condition()
}
