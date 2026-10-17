package gate

import (
	"context"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
)

func TestControllerClientDecideFlow(t *testing.T) {
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	path := startControlServer(t, coordinator)

	client, err := DialController(path)
	if err != nil {
		t.Fatalf("DialController() error = %v", err)
	}
	defer func() {
		_ = client.Close()
	}()
	if client.Timeout() != time.Minute {
		t.Fatalf("Timeout() = %s, want 1m", client.Timeout())
	}
	if client.RememberDefault() != time.Minute {
		t.Fatalf("RememberDefault() = %s, want 1m", client.RememberDefault())
	}
	if len(client.Pending()) != 0 {
		t.Fatalf("Pending() = %+v, want empty", client.Pending())
	}

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(t.Context(), testRequest("SHA256:client"))
		results <- result
	}()

	pending := nextControllerEvent(t, client, "pending")
	if pending.Request == nil || pending.Request.KeyFingerprint != "SHA256:client" {
		t.Fatalf("pending event = %+v, want request with SHA256:client", pending.Request)
	}

	if err := client.Decide(pending.Request.ID, true, 0, 0); err != nil {
		t.Fatalf("Decide() error = %v", err)
	}

	ok := nextControllerEvent(t, client, "ok")
	if ok.ID != pending.Request.ID {
		t.Fatalf("ok id = %q, want %q", ok.ID, pending.Request.ID)
	}
	resolved := nextControllerEvent(t, client, "resolved")
	if resolved.Outcome != "allowed" || resolved.Source != "controller" {
		t.Fatalf("resolved = %s/%s, want allowed/controller", resolved.Outcome, resolved.Source)
	}
	if result := <-results; result.Outcome != OutcomeAllowed {
		t.Fatalf("Submit() result = %+v, want allowed", result)
	}
}

func TestControllerClientClosedServer(t *testing.T) {
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	controlServer := &ControlServer{
		Coordinator: coordinator,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go func() {
		_ = controlServer.Serve(ctx, listener)
	}()

	client, err := DialController(path)
	if err != nil {
		t.Fatalf("DialController() error = %v", err)
	}
	cancel()
	if _, ok := <-client.Events(); ok {
		t.Fatal("Events() delivered an event after server shutdown, want closed channel")
	}
}

func TestControllerClientDialFailure(t *testing.T) {
	if _, err := DialController(filepath.Join(t.TempDir(), "missing.sock")); err == nil {
		t.Fatal("DialController() error = nil, want error for missing socket")
	}
}

func nextControllerEvent(t *testing.T, client *ControllerClient, eventType string) ControllerEvent {
	t.Helper()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		select {
		case event, ok := <-client.Events():
			if !ok {
				t.Fatalf("events channel closed while waiting for %s", eventType)
			}
			if event.Type == eventType {
				return event
			}
		case <-deadline.C:
			t.Fatalf("event %s never arrived", eventType)
		}
	}
}
