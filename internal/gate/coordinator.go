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
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
)

const eventChannelSize = 64

type Request struct {
	ID             string            `json:"id"`
	KeyFormat      string            `json:"key_format"`
	KeyFingerprint string            `json:"key_fingerprint"`
	KeyComment     string            `json:"key_comment,omitempty"`
	ClientPID      int               `json:"client_pid"`
	ClientExe      string            `json:"client_exe,omitempty"`
	ClientCmdline  string            `json:"client_cmdline,omitempty"`
	ParentChain    string            `json:"parent_chain,omitempty"`
	ReceivedAt     time.Time         `json:"received_at"`
	Deadline       time.Time         `json:"deadline"`
	Scopes         []process.Summary `json:"scopes,omitempty"`
	DefaultScope   int               `json:"default_scope"`
	ScopeAutomatic bool              `json:"scope_automatic,omitempty"`
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

type grantKey struct {
	Fingerprint string
	Process     process.Instance
}

func (request Request) hasScope(instance process.Instance) bool {
	for _, scope := range request.Scopes {
		if scope.Instance() == instance {
			return true
		}
	}
	return false
}

type Coordinator struct {
	timeout     time.Duration
	rememberTTL time.Duration
	notifier    notify.Notifier
	logger      *slog.Logger

	mu          sync.Mutex
	nextID      int
	pending     map[string]*pendingEntry
	remember    map[grantKey]time.Time
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
		remember:    map[grantKey]time.Time{},
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
	if scope, remembered := coordinator.rememberedLocked(request, now); remembered {
		coordinator.mu.Unlock()
		coordinator.logger.Info(
			"signature request allowed by remembered decision",
			"key_name", request.KeyName(),
			"key_fingerprint", request.KeyFingerprint,
			"client_pid", request.ClientPID,
			"client_exe", request.ClientExe,
			"client_cmdline", request.ClientCmdline,
			"scope_pid", scope.PID,
			"scope_start_time", scope.StartTime,
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
		"command_scopes", request.Scopes,
		"default_scope", request.DefaultScope,
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

// Decide allows or denies a pending signature. Remembering an allow covers
// the selected command instance and its descendants for this key only.
// One-shot decisions cover requests from the same requester instance.
func (coordinator *Coordinator) Decide(id string, allow bool, remember time.Duration, scopeIndex int) error {
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
	remembering := allow && remember > 0 && coordinator.rememberTTL > 0
	var instance process.Instance
	if remembering {
		if scopeIndex < 0 || scopeIndex >= len(entry.request.Scopes) {
			return fmt.Errorf("invalid command scope %d", scopeIndex)
		}
		scope := entry.request.Scopes[scopeIndex]
		instance = scope.Instance()
		if !instance.Alive() {
			return fmt.Errorf("command scope pid %d has exited", instance.PID)
		}
		coordinator.remember[grantKey{Fingerprint: entry.request.KeyFingerprint, Process: instance}] = time.Now().Add(remember)
		coordinator.logger.Info(
			"remembered allow decision",
			"remember_ttl", remember,
			"key_name", entry.request.KeyName(),
			"key_fingerprint", entry.request.KeyFingerprint,
			"scope_pid", scope.PID,
			"scope_start_time", scope.StartTime,
			"scope_cmdline", process.CommandLine(scope.Cmdline),
		)
	} else if len(entry.request.Scopes) > 0 && entry.request.Scopes[0].PID == entry.request.ClientPID {
		instance = entry.request.Scopes[0].Instance()
	}
	resolved := 0
	for _, candidate := range coordinator.pending {
		sameScope := instance.StartTime != 0 && candidate.request.KeyFingerprint == entry.request.KeyFingerprint && candidate.request.hasScope(instance)
		if !remembering {
			sameScope = sameScope && candidate.request.ClientPID == entry.request.ClientPID
		}
		if candidate.resolved || (candidate != entry && !sameScope) {
			continue
		}
		coordinator.resolveLocked(candidate, Result{Outcome: outcome, Source: SourceController})
		resolved++
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

// rememberedLocked requires both an unexpired grant and its original live
// process instance. The caller must hold coordinator.mu.
func (coordinator *Coordinator) rememberedLocked(request Request, now time.Time) (process.Instance, bool) {
	for key, expiry := range coordinator.remember {
		if !now.Before(expiry) || !key.Process.Alive() {
			delete(coordinator.remember, key)
			continue
		}
		if key.Fingerprint == request.KeyFingerprint && request.hasScope(key.Process) {
			return key.Process, true
		}
	}
	return process.Instance{}, false
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
