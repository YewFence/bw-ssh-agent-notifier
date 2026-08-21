package gate

import (
	"context"
	"fmt"
	"log/slog"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
)

const eventChannelSize = 64

type Request struct {
	ID             string    `json:"id"`
	KeyFormat      string    `json:"key_format"`
	KeyFingerprint string    `json:"key_fingerprint"`
	KeyComment     string    `json:"key_comment,omitempty"`
	ClientPID      int       `json:"client_pid"`
	ClientExe      string    `json:"client_exe,omitempty"`
	ClientCmdline  string    `json:"client_cmdline,omitempty"`
	ParentChain    string    `json:"parent_chain,omitempty"`
	ReceivedAt     time.Time `json:"received_at"`
	Deadline       time.Time `json:"deadline"`
	MemoryKey      string    `json:"-"`
}

func (request Request) KeyName() string {
	if request.KeyComment != "" {
		return request.KeyComment
	}
	return request.KeyFingerprint
}

func (request Request) ClientName() string {
	if request.ClientExe != "" {
		return filepath.Base(request.ClientExe)
	}
	if fields := strings.Fields(request.ClientCmdline); len(fields) > 0 {
		return filepath.Base(fields[0])
	}
	if request.ClientPID > 0 {
		return fmt.Sprintf("pid %d", request.ClientPID)
	}
	return "unknown process"
}

type Outcome string

const (
	OutcomeAllowed Outcome = "allowed"
	OutcomeDenied  Outcome = "denied"
)

type Source string

const (
	SourceController   Source = "controller"
	SourceMemory       Source = "memory"
	SourceTimeout      Source = "timeout"
	SourceNoController Source = "no-controller"
	SourceShutdown     Source = "shutdown"
)

type Result struct {
	Outcome Outcome
	Source  Source
}

func (result Result) Err() error {
	if result.Outcome == OutcomeAllowed {
		return nil
	}
	return fmt.Errorf("signature request %s (%s)", result.Outcome, result.Source)
}

type EventKind string

const (
	EventPending  EventKind = "pending"
	EventResolved EventKind = "resolved"
)

type Event struct {
	Kind    EventKind
	Request Request
	ID      string
	Result  Result
}

type pendingEntry struct {
	request  Request
	resolved bool
	result   Result
	ready    chan Result
}

type Coordinator struct {
	timeout     time.Duration
	rememberTTL time.Duration
	notifier    notify.Notifier
	logger      *slog.Logger

	mu          sync.Mutex
	nextID      int
	pending     map[string]*pendingEntry
	remember    map[string]time.Time
	controllers int
	nextSubID   int
	subscribers map[int]chan Event
}

func NewCoordinator(timeout time.Duration, rememberTTL time.Duration, notifier notify.Notifier, logger *slog.Logger) *Coordinator {
	if notifier == nil {
		notifier = notify.Noop()
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Coordinator{
		timeout:     timeout,
		rememberTTL: rememberTTL,
		notifier:    notifier,
		logger:      logger,
		pending:     map[string]*pendingEntry{},
		remember:    map[string]time.Time{},
		subscribers: map[int]chan Event{},
	}
}

func (coordinator *Coordinator) Timeout() time.Duration {
	return coordinator.timeout
}

func (coordinator *Coordinator) RememberTTL() time.Duration {
	return coordinator.rememberTTL
}

func (coordinator *Coordinator) Submit(ctx context.Context, request Request) (Result, error) {
	now := time.Now()

	coordinator.mu.Lock()
	if coordinator.rememberedLocked(request.MemoryKey, now) {
		coordinator.mu.Unlock()
		coordinator.logger.Info(
			"signature request allowed by remembered decision",
			"key_name", request.KeyName(),
			"key_fingerprint", request.KeyFingerprint,
			"client_pid", request.ClientPID,
			"client_exe", request.ClientExe,
			"client_cmdline", request.ClientCmdline,
		)
		return Result{Outcome: OutcomeAllowed, Source: SourceMemory}, nil
	}
	coordinator.nextID++
	request.ID = strconv.Itoa(coordinator.nextID)
	request.ReceivedAt = now
	request.Deadline = now.Add(coordinator.timeout)
	entry := &pendingEntry{request: request, ready: make(chan Result, 1)}
	ringDoorbell := !coordinator.hasPendingFingerprint(request.KeyFingerprint)
	coordinator.pending[request.ID] = entry
	coordinator.broadcast(Event{Kind: EventPending, Request: request})
	coordinator.mu.Unlock()

	coordinator.logger.Info(
		"signature request pending",
		"request_id", request.ID,
		"key_name", request.KeyName(),
		"key_fingerprint", request.KeyFingerprint,
		"client_pid", request.ClientPID,
		"client_exe", request.ClientExe,
		"client_cmdline", request.ClientCmdline,
	)
	if ringDoorbell {
		coordinator.notifyPending(request)
	}

	timer := time.NewTimer(coordinator.timeout)
	defer timer.Stop()
	var result Result
	select {
	case result = <-entry.ready:
	case <-timer.C:
		result = coordinator.resolve(request.ID, Result{Outcome: OutcomeDenied, Source: SourceTimeout})
	case <-ctx.Done():
		result = coordinator.resolve(request.ID, Result{Outcome: OutcomeDenied, Source: SourceShutdown})
	}

	coordinator.finish(request, result)
	return result, result.Err()
}

// Decide resolves the pending request with the given id. The decision also
// applies to every other pending request sharing its memory key, so a burst of
// identical requests clears with one action. A positive remember duration
// additionally stores an allow decision for Submit to replay until it expires.
func (coordinator *Coordinator) Decide(id string, allow bool, remember time.Duration) error {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	entry, ok := coordinator.pending[id]
	if !ok {
		return fmt.Errorf("unknown request id %q", id)
	}
	if entry.resolved {
		return fmt.Errorf("request %q is already resolved", id)
	}
	outcome := OutcomeDenied
	if allow {
		outcome = OutcomeAllowed
	}
	resolved := 0
	for _, candidate := range coordinator.pending {
		sameKey := entry.request.MemoryKey != "" && candidate.request.MemoryKey == entry.request.MemoryKey
		if candidate.resolved || (candidate != entry && !sameKey) {
			continue
		}
		coordinator.resolveLocked(candidate, Result{Outcome: outcome, Source: SourceController})
		resolved++
	}
	if allow && remember > 0 && coordinator.rememberTTL > 0 && entry.request.MemoryKey != "" {
		coordinator.remember[entry.request.MemoryKey] = time.Now().Add(remember)
		coordinator.logger.Info(
			"remembered allow decision",
			"remember_ttl", remember,
			"key_name", entry.request.KeyName(),
			"client_cmdline", entry.request.ClientCmdline,
		)
	}
	coordinator.logger.Info("controller decision applied", "request_id", id, "outcome", string(outcome), "resolved", resolved)
	return nil
}

func (coordinator *Coordinator) Pending() []Request {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	requests := make([]Request, 0, len(coordinator.pending))
	for _, entry := range coordinator.pending {
		requests = append(requests, entry.request)
	}
	sort.Slice(requests, func(i, j int) bool {
		left, leftErr := strconv.Atoi(requests[i].ID)
		right, rightErr := strconv.Atoi(requests[j].ID)
		if leftErr != nil || rightErr != nil {
			return requests[i].ID < requests[j].ID
		}
		return left < right
	})
	return requests
}

func (coordinator *Coordinator) ControllerConnected() {
	coordinator.mu.Lock()
	coordinator.controllers++
	coordinator.mu.Unlock()
}

func (coordinator *Coordinator) ControllerDisconnected() {
	coordinator.mu.Lock()
	if coordinator.controllers > 0 {
		coordinator.controllers--
	}
	coordinator.mu.Unlock()
}

func (coordinator *Coordinator) Subscribe() (<-chan Event, func()) {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	subID := coordinator.nextSubID
	coordinator.nextSubID++
	events := make(chan Event, eventChannelSize)
	coordinator.subscribers[subID] = events
	cancel := func() {
		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()
		if _, ok := coordinator.subscribers[subID]; ok {
			delete(coordinator.subscribers, subID)
			close(events)
		}
	}
	return events, cancel
}

func (coordinator *Coordinator) resolve(id string, result Result) Result {
	coordinator.mu.Lock()
	defer coordinator.mu.Unlock()
	entry, ok := coordinator.pending[id]
	if !ok || entry.resolved {
		return result
	}
	coordinator.resolveLocked(entry, result)
	return entry.result
}

func (coordinator *Coordinator) resolveLocked(entry *pendingEntry, result Result) {
	entry.resolved = true
	entry.result = result
	entry.ready <- result
	coordinator.broadcast(Event{Kind: EventResolved, ID: entry.request.ID, Result: result})
}

func (coordinator *Coordinator) finish(request Request, result Result) {
	coordinator.mu.Lock()
	delete(coordinator.pending, request.ID)
	coordinator.mu.Unlock()

	coordinator.logger.Info(
		"signature request resolved",
		"request_id", request.ID,
		"outcome", string(result.Outcome),
		"source", string(result.Source),
		"key_name", request.KeyName(),
		"key_fingerprint", request.KeyFingerprint,
		"client_pid", request.ClientPID,
		"client_exe", request.ClientExe,
		"client_cmdline", request.ClientCmdline,
	)
	coordinator.notifyResult(request, result)
}

func (coordinator *Coordinator) hasPendingFingerprint(fingerprint string) bool {
	for _, entry := range coordinator.pending {
		if entry.request.KeyFingerprint == fingerprint {
			return true
		}
	}
	return false
}

// rememberedLocked reports whether a still-valid remembered decision covers
// the memory key; expired entries are dropped on the way. The caller must hold
// coordinator.mu.
func (coordinator *Coordinator) rememberedLocked(memoryKey string, now time.Time) bool {
	if memoryKey == "" {
		return false
	}
	expiry, ok := coordinator.remember[memoryKey]
	if !ok {
		return false
	}
	if now.After(expiry) {
		delete(coordinator.remember, memoryKey)
		return false
	}
	return true
}

// broadcast delivers event to every subscriber without blocking; the caller
// must hold coordinator.mu. A controller that falls behind loses events and
// resynchronizes through the pending snapshot in its next state message.
func (coordinator *Coordinator) broadcast(event Event) {
	for _, events := range coordinator.subscribers {
		select {
		case events <- event:
		default:
		}
	}
}

func (coordinator *Coordinator) notifyPending(request Request) {
	notification := notify.Notification{Summary: "SSH signature request", Body: pendingBody(request)}
	if err := coordinator.notifier.Send(context.Background(), notification); err != nil {
		coordinator.logger.Warn("send pending notification failed", "error", err)
	}
}

func (coordinator *Coordinator) notifyResult(request Request, result Result) {
	summary := "SSH signature allowed"
	if result.Outcome == OutcomeDenied {
		summary = "SSH signature denied"
	}
	notification := notify.Notification{Summary: summary, Body: resultBody(request, result)}
	if err := coordinator.notifier.Send(context.Background(), notification); err != nil {
		coordinator.logger.Warn("send result notification failed", "error", err)
	}
}

func pendingBody(request Request) string {
	body := fmt.Sprintf("%s requests signature with %s", request.ClientName(), request.KeyName())
	if request.ClientPID > 0 {
		body = fmt.Sprintf("%s\nPID %d", body, request.ClientPID)
	}
	if request.ClientCmdline != "" {
		body = fmt.Sprintf("%s · %s", body, request.ClientCmdline)
	}
	if request.ParentChain != "" {
		body = fmt.Sprintf("%s\nProcess tree %s", body, request.ParentChain)
	}
	return body
}

func resultBody(request Request, result Result) string {
	var reason string
	switch result.Source {
	case SourceController:
		reason = "decision made at gate controller"
	case SourceTimeout:
		reason = "timed out waiting for decision"
	case SourceNoController:
		reason = "no gate controller connected"
	case SourceShutdown:
		reason = "gate is shutting down"
	}
	return fmt.Sprintf("%s · %s\n%s", request.ClientName(), request.KeyName(), reason)
}
