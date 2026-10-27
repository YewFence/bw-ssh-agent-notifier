package gate

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
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
	info, err := process.Inspect(os.Getpid(), 0)
	if err != nil {
		panic(err)
	}
	return Request{
		KeyFormat: "ssh-ed25519", KeyFingerprint: fingerprint, KeyComment: "test-key",
		ClientPID: info.PID, ClientExe: info.Exe, ClientCmdline: process.CommandLine(info.Cmdline),
		Scopes: process.CommandScopes(info),
	}
}

func submitPending(t *testing.T, coordinator *Coordinator, request Request) (Request, <-chan Result) {
	t.Helper()
	events, cancel := coordinator.Subscribe()
	defer cancel()
	results := make(chan Result, 1)
	go func() {
		result, err := coordinator.Submit(t.Context(), request)
		if (err == nil) != (result.Outcome == OutcomeAllowed) {
			t.Errorf("Submit() = (%+v, %v), inconsistent outcome and error", result, err)
		}
		results <- result
	}()
	pending := nextEvent(t, events, EventPending)
	return pending.Request, results
}

func TestSubmitDeniedWithoutController(t *testing.T) {
	notifier := &countingNotifier{}
	coordinator := newTestCoordinator(30*time.Millisecond, notifier)
	result, err := coordinator.Submit(t.Context(), testRequest("SHA256:aaa"))
	if result.Outcome != OutcomeDenied || result.Source != SourceTimeout || err == nil {
		t.Fatalf("Submit() = %+v, %v; want denied/timeout", result, err)
	}
	if got := notifier.count("SSH signature denied"); got != 1 {
		t.Fatalf("denied notifications = %d, want 1", got)
	}
	if got := notifier.count("SSH signature request"); got != 1 {
		t.Fatalf("pending notifications = %d, want 1", got)
	}
}

func TestSubmitControllerDecision(t *testing.T) {
	for _, allow := range []bool{false, true} {
		coordinator := newTestCoordinator(time.Second, notify.Noop())
		events, cancel := coordinator.Subscribe()
		pending, results := submitPending(t, coordinator, testRequest("SHA256:bbb"))
		if pending.KeyComment != "test-key" {
			t.Fatalf("pending comment = %q, want test-key", pending.KeyComment)
		}
		if err := coordinator.Decide(pending.ID, allow, 0, 0); err != nil {
			t.Fatal(err)
		}
		want := OutcomeDenied
		if allow {
			want = OutcomeAllowed
		}
		if result := <-results; result.Outcome != want || result.Source != SourceController {
			t.Fatalf("Submit() = %+v, want %s/controller", result, want)
		}
		resolved := nextEvent(t, events, EventResolved)
		if resolved.ID != pending.ID || resolved.Result.Outcome != want {
			t.Fatalf("resolved = %+v, want %s/%s", resolved, pending.ID, want)
		}
		cancel()
	}
}

func TestPendingSnapshotExcludesResolvedEntries(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	request := testRequest("SHA256:resolved")
	request.ID = "1"
	coordinator.pending[request.ID] = &pendingEntry{request: request, resolved: true}
	if requests := coordinator.Pending(); len(requests) != 0 {
		t.Fatalf("resolved request in pending snapshot: %+v", requests)
	}
	if coordinator.hasPendingFingerprint(request.KeyFingerprint) {
		t.Fatal("resolved request suppresses a new notification")
	}
}

func TestSubmitTimeout(t *testing.T) {
	coordinator := newTestCoordinator(30*time.Millisecond, notify.Noop())
	start := time.Now()
	result, err := coordinator.Submit(t.Context(), testRequest("SHA256:ccc"))
	if result.Outcome != OutcomeDenied || result.Source != SourceTimeout || err == nil {
		t.Fatalf("Submit() = %+v, %v; want denied/timeout", result, err)
	}
	if elapsed := time.Since(start); elapsed < 25*time.Millisecond {
		t.Fatalf("Submit() returned after %s, want timeout wait", elapsed)
	}
}

func TestSubmitShutdownContext(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	ctx, cancel := context.WithCancel(t.Context())
	events, cancelEvents := coordinator.Subscribe()
	defer cancelEvents()
	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(ctx, testRequest("SHA256:ddd"))
		results <- result
	}()
	nextEvent(t, events, EventPending)
	cancel()
	if result := <-results; result.Outcome != OutcomeDenied || result.Source != SourceShutdown {
		t.Fatalf("Submit() = %+v, want denied/shutdown", result)
	}
}

func TestDecideUnknownRequest(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	if err := coordinator.Decide("404", true, 0, 0); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestSubmitAllowedByRememberedDecision(t *testing.T) {
	notifier := &countingNotifier{}
	coordinator := newTestCoordinator(time.Second, notifier)
	request := testRequest("SHA256:mem")
	pending, results := submitPending(t, coordinator, request)
	if err := coordinator.Decide(pending.ID, true, time.Minute, 0); err != nil {
		t.Fatal(err)
	}
	if result := <-results; result.Source != SourceController {
		t.Fatalf("first decision = %+v, want controller", result)
	}
	replay, err := coordinator.Submit(t.Context(), request)
	if replay.Outcome != OutcomeAllowed || replay.Source != SourceMemory || err != nil {
		t.Fatalf("replay = %+v, %v; want allowed/memory", replay, err)
	}
	if got := notifier.count("SSH signature request"); got != 1 {
		t.Fatalf("pending notifications = %d, want 1", got)
	}
}

func TestRememberedDecisionExpires(t *testing.T) {
	coordinator := newTestCoordinator(30*time.Millisecond, notify.Noop())
	request := testRequest("SHA256:exp")
	pending, results := submitPending(t, coordinator, request)
	if err := coordinator.Decide(pending.ID, true, time.Millisecond, 0); err != nil {
		t.Fatal(err)
	}
	if result := <-results; result.Source != SourceController {
		t.Fatalf("first decision = %+v, want controller", result)
	}
	time.Sleep(5 * time.Millisecond)
	replay, err := coordinator.Submit(t.Context(), request)
	if replay.Outcome != OutcomeDenied || replay.Source != SourceTimeout || err == nil {
		t.Fatalf("expired replay = %+v, %v; want denied/timeout", replay, err)
	}
}

func TestRememberDisabledIgnoresControllerRemember(t *testing.T) {
	coordinator := NewCoordinator(30*time.Millisecond, 0, notify.Noop(), slog.New(slog.NewTextHandler(io.Discard, nil)))
	request := testRequest("SHA256:off")
	pending, results := submitPending(t, coordinator, request)
	if err := coordinator.Decide(pending.ID, true, time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	<-results
	replay, err := coordinator.Submit(t.Context(), request)
	if replay.Outcome != OutcomeDenied || replay.Source != SourceTimeout || err == nil {
		t.Fatalf("replay = %+v, %v; want denied/timeout", replay, err)
	}
}

func TestDecideDenyDoesNotRemember(t *testing.T) {
	coordinator := newTestCoordinator(30*time.Millisecond, notify.Noop())
	request := testRequest("SHA256:dny")
	pending, results := submitPending(t, coordinator, request)
	if err := coordinator.Decide(pending.ID, false, time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	if result := <-results; result.Outcome != OutcomeDenied {
		t.Fatalf("first decision = %+v, want denied", result)
	}
	replay, err := coordinator.Submit(t.Context(), request)
	if replay.Outcome != OutcomeDenied || replay.Source != SourceTimeout || err == nil {
		t.Fatalf("replay = %+v, %v; want denied/timeout", replay, err)
	}
}

func TestDecideBatchResolvesSameRequesterAndKey(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	first, results1 := submitPending(t, coordinator, testRequest("SHA256:batch"))
	second, results2 := submitPending(t, coordinator, testRequest("SHA256:batch"))
	if err := coordinator.Decide(first.ID, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	for _, results := range []<-chan Result{results1, results2} {
		if result := <-results; result.Outcome != OutcomeAllowed || result.Source != SourceController {
			t.Fatalf("batch result = %+v, want allowed/controller", result)
		}
	}
	if err := coordinator.Decide(second.ID, true, 0, 0); err == nil {
		t.Fatal("expected error for already resolved request")
	}
}

func TestDecideTwiceRejects(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	pending, results := submitPending(t, coordinator, testRequest("SHA256:twice"))
	if err := coordinator.Decide(pending.ID, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	if err := coordinator.Decide(pending.ID, true, 0, 0); err == nil {
		t.Fatal("expected error for already resolved request")
	}
	<-results
}

func TestDoorbellRingsOncePerFingerprint(t *testing.T) {
	notifier := &countingNotifier{}
	coordinator := newTestCoordinator(time.Second, notifier)
	first, results1 := submitPending(t, coordinator, testRequest("SHA256:bell"))
	_, results2 := submitPending(t, coordinator, testRequest("SHA256:bell"))
	if err := coordinator.Decide(first.ID, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	<-results1
	<-results2
	if got := notifier.count("SSH signature request"); got != 1 {
		t.Fatalf("pending notifications = %d, want 1", got)
	}
}

func TestRememberedAncestorCoversDifferentHelpers(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	owner := testRequest("SHA256:scope").Scopes[0]
	request := testRequest("SHA256:scope")
	request.ClientPID = 4242
	request.Scopes = []process.Summary{
		{PID: 4242, StartTime: 100, Exe: "/usr/bin/ssh", Cmdline: []string{"ssh", "fetch"}},
		{PID: 4241, StartTime: 90, Exe: "/usr/bin/git", Cmdline: []string{"git", "ls-remote"}},
		owner,
	}
	first, results1 := submitPending(t, coordinator, request)
	sibling := request
	sibling.ClientPID = 4250
	sibling.Scopes = []process.Summary{{PID: 4250, StartTime: 110, Exe: "/usr/bin/ssh-keygen"}, owner}
	_, results2 := submitPending(t, coordinator, sibling)
	if err := coordinator.Decide(first.ID, true, time.Minute, 2); err != nil {
		t.Fatal(err)
	}
	for _, results := range []<-chan Result{results1, results2} {
		if result := <-results; result.Outcome != OutcomeAllowed || result.Source != SourceController {
			t.Fatalf("batch result = %+v, want allowed/controller", result)
		}
	}
	replay := sibling
	replay.ClientPID = 4260
	replay.Scopes = []process.Summary{{PID: 4260, StartTime: 120, Exe: "/usr/bin/ssh"}, owner}
	result, err := coordinator.Submit(t.Context(), replay)
	if result.Source != SourceMemory || err != nil {
		t.Fatalf("different helper replay = %+v, %v; want memory", result, err)
	}
}

func TestRememberedScopeDoesNotCoverOtherKeysOrInstances(t *testing.T) {
	for _, mutation := range []string{"key", "pid", "start time", "missing ancestry"} {
		t.Run(mutation, func(t *testing.T) {
			coordinator := newTestCoordinator(30*time.Millisecond, notify.Noop())
			request := testRequest("SHA256:original")
			pending, results := submitPending(t, coordinator, request)
			if err := coordinator.Decide(pending.ID, true, time.Minute, 0); err != nil {
				t.Fatal(err)
			}
			<-results
			replay := testRequest("SHA256:original")
			switch mutation {
			case "key":
				replay.KeyFingerprint = "SHA256:other"
			case "pid":
				replay.Scopes[0].PID++
			case "start time":
				replay.Scopes[0].StartTime++
			case "missing ancestry":
				replay.Scopes = nil
			}
			result, err := coordinator.Submit(t.Context(), replay)
			if result.Outcome != OutcomeDenied || result.Source != SourceTimeout || err == nil {
				t.Fatalf("unrelated replay = %+v, %v; want denied/timeout", result, err)
			}
		})
	}
}

func TestRememberedScopeEndsWhenOwnerExits(t *testing.T) {
	command := exec.Command("sleep", "60")
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = command.Process.Kill(); _ = command.Wait() })
	info, err := process.Inspect(command.Process.Pid, 0)
	if err != nil {
		t.Fatal(err)
	}
	request := testRequest("SHA256:exit")
	request.Scopes = process.CommandScopes(info)
	request.ClientPID = info.PID
	coordinator := newTestCoordinator(30*time.Millisecond, notify.Noop())
	pending, results := submitPending(t, coordinator, request)
	if err := coordinator.Decide(pending.ID, true, time.Hour, 0); err != nil {
		t.Fatal(err)
	}
	<-results
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	result, err := coordinator.Submit(t.Context(), request)
	if result.Outcome != OutcomeDenied || result.Source != SourceTimeout || err == nil {
		t.Fatalf("exited owner replay = %+v, %v; want denied/timeout", result, err)
	}
}

func TestDecideRejectsInvalidOrExitedScope(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	request := testRequest("SHA256:invalid")
	request.Scopes[0].StartTime++
	pending, results := submitPending(t, coordinator, request)
	for _, scope := range []int{-1, 1, 0} {
		err := coordinator.Decide(pending.ID, true, time.Minute, scope)
		if err == nil {
			t.Fatalf("expected invalid/exited scope error for %d", scope)
		}
		if scope == 0 && !strings.Contains(err.Error(), "select a live scope or allow once") {
			t.Fatalf("exited scope error = %q, missing recovery guidance", err)
		}
	}
	if err := coordinator.Decide(pending.ID, false, 0, 0); err != nil {
		t.Fatal(err)
	}
	if result := <-results; result.Outcome != OutcomeDenied {
		t.Fatalf("invalid scope request = %+v, want denied", result)
	}
	pending, results = submitPending(t, coordinator, request)
	if err := coordinator.Decide(pending.ID, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	if result := <-results; result.Outcome != OutcomeAllowed {
		t.Fatalf("invalid scope request = %+v, want allowed once", result)
	}
}

func TestDecideExitedAncestorCanSelectLiveRequester(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	request := testRequest("SHA256:ancestor-exited")
	exited := request.Scopes[0]
	exited.StartTime++
	request.Scopes = append(request.Scopes, exited)
	pending, results := submitPending(t, coordinator, request)
	if err := coordinator.Decide(pending.ID, true, time.Minute, 1); err == nil {
		t.Fatal("expected error for exited ancestor")
	}
	if err := coordinator.Decide(pending.ID, true, time.Minute, 0); err != nil {
		t.Fatalf("remember live requester: %v", err)
	}
	if result := <-results; result.Outcome != OutcomeAllowed || result.Source != SourceController {
		t.Fatalf("result = %+v, want allowed/controller", result)
	}
	replay, err := coordinator.Submit(t.Context(), request)
	if replay.Outcome != OutcomeAllowed || replay.Source != SourceMemory || err != nil {
		t.Fatalf("replay = %+v, %v; want allowed/memory", replay, err)
	}
}

func TestOneShotDoesNotAuthorizeOtherKeysOrSiblingCommands(t *testing.T) {
	for _, otherKey := range []bool{false, true} {
		coordinator := newTestCoordinator(time.Second, notify.Noop())
		request := testRequest("SHA256:one-shot")
		first, results1 := submitPending(t, coordinator, request)
		other := testRequest("SHA256:one-shot")
		if otherKey {
			other.KeyFingerprint = "SHA256:other-key"
		} else {
			other.ClientPID++
			other.Scopes = append([]process.Summary{{PID: other.ClientPID, StartTime: 123}}, other.Scopes...)
		}
		second, results2 := submitPending(t, coordinator, other)
		if err := coordinator.Decide(first.ID, true, 0, 0); err != nil {
			t.Fatal(err)
		}
		if result := <-results1; result.Outcome != OutcomeAllowed {
			t.Fatalf("first result = %+v, want allowed", result)
		}
		if err := coordinator.Decide(second.ID, false, 0, 0); err != nil {
			t.Fatalf("unrelated request was unexpectedly resolved: %v", err)
		}
		if result := <-results2; result.Outcome != OutcomeDenied {
			t.Fatalf("unrelated result = %+v, want denied", result)
		}
	}
}

func nextEvent(t *testing.T, events <-chan Event, kind EventKind) Event {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case event := <-events:
			if event.Kind == kind {
				return event
			}
		case <-timer.C:
			t.Fatalf("event %s never arrived", kind)
			return Event{}
		}
	}
}
