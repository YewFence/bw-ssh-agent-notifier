package gate

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
)

func startControlServer(t *testing.T, coordinator *Coordinator) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "control.sock")
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	controlServer := &ControlServer{
		Coordinator: coordinator,
		Logger:      slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = controlServer.Serve(t.Context(), listener)
	}()
	t.Cleanup(func() {
		_ = listener.Close()
		<-done
	})
	return path
}

type controlClient struct {
	conn   net.Conn
	reader *bufio.Reader
}

func dialControl(t *testing.T, path string) *controlClient {
	t.Helper()
	conn, err := net.Dial("unix", path)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	t.Cleanup(func() {
		_ = conn.Close()
	})
	return &controlClient{conn: conn, reader: bufio.NewReader(conn)}
}

func (client *controlClient) read(t *testing.T) serverMessage {
	t.Helper()
	line, err := client.reader.ReadString('\n')
	if err != nil {
		t.Fatalf("read line error = %v", err)
	}
	var message serverMessage
	if err := json.Unmarshal([]byte(line), &message); err != nil {
		t.Fatalf("unmarshal %q error = %v", line, err)
	}
	return message
}

func (client *controlClient) readUntil(t *testing.T, messageType string) serverMessage {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		message := client.read(t)
		if message.Type == messageType {
			return message
		}
		if time.Now().After(deadline) {
			t.Fatalf("message %s not seen in time", messageType)
		}
	}
}

func (client *controlClient) send(t *testing.T, command controllerCommand) {
	t.Helper()
	encoder := json.NewEncoder(client.conn)
	if err := encoder.Encode(command); err != nil {
		t.Fatalf("send %v error = %v", command, err)
	}
}

func TestControlProtocolDecideFlow(t *testing.T) {
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	path := startControlServer(t, coordinator)
	client := dialControl(t, path)

	hello := client.read(t)
	if hello.Type != "hello" {
		t.Fatalf("first message type = %q, want hello", hello.Type)
	}
	if hello.TimeoutSeconds != 60 {
		t.Fatalf("hello timeout_seconds = %d, want 60", hello.TimeoutSeconds)
	}

	results := make(chan Result, 1)
	go func() {
		result, _ := coordinator.Submit(t.Context(), testRequest("SHA256:ctl"))
		results <- result
	}()

	pending := client.readUntil(t, "pending")
	if pending.Request == nil {
		t.Fatal("pending message has no request")
	}
	if pending.Request.KeyFingerprint != "SHA256:ctl" {
		t.Fatalf("pending fingerprint = %q, want SHA256:ctl", pending.Request.KeyFingerprint)
	}

	client.send(t, controllerCommand{Type: "decide", ID: pending.Request.ID, Decision: "allow"})

	sawOK, sawResolved := false, false
	for !sawOK || !sawResolved {
		message := client.read(t)
		switch message.Type {
		case "ok":
			if message.ID != pending.Request.ID {
				t.Fatalf("ok id = %q, want %q", message.ID, pending.Request.ID)
			}
			sawOK = true
		case "resolved":
			if message.Outcome != "allowed" || message.Source != "controller" {
				t.Fatalf("resolved = %s/%s, want allowed/controller", message.Outcome, message.Source)
			}
			sawResolved = true
		case "error":
			t.Fatalf("unexpected error message %q", message.Error)
		}
	}

	result := <-results
	if result.Outcome != OutcomeAllowed || result.Source != SourceController {
		t.Fatalf("Submit() result = %+v, want allowed/controller", result)
	}
}

func TestControlProtocolListCommand(t *testing.T) {
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	path := startControlServer(t, coordinator)
	client := dialControl(t, path)
	client.read(t)

	client.send(t, controllerCommand{Type: "list"})

	state := client.readUntil(t, "state")
	if len(state.Requests) != 0 {
		t.Fatalf("state requests = %+v, want empty", state.Requests)
	}
}

func TestControlProtocolRememberRoundTrip(t *testing.T) {
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	path := startControlServer(t, coordinator)
	client := dialControl(t, path)
	client.read(t)

	results := make(chan Result, 2)
	go func() {
		result, _ := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:mem", "mem-X"))
		results <- result
	}()
	pending := client.readUntil(t, "pending")

	client.send(t, controllerCommand{Type: "decide", ID: pending.Request.ID, Decision: "allow", RememberSeconds: 120})
	client.readUntil(t, "ok")
	if result := <-results; result.Source != SourceController {
		t.Fatalf("first Submit() result = %+v, want controller decision", result)
	}

	go func() {
		result, _ := coordinator.Submit(t.Context(), requestWithMemoryKey("SHA256:mem", "mem-X"))
		results <- result
	}()
	select {
	case result := <-results:
		if result.Outcome != OutcomeAllowed || result.Source != SourceMemory {
			t.Fatalf("replay Submit() = %+v, want allowed/memory", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("replay Submit() blocked, remembered decision not applied")
	}
}

func TestControlProtocolInvalidDecision(t *testing.T) {
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	path := startControlServer(t, coordinator)
	client := dialControl(t, path)
	client.read(t)

	client.send(t, controllerCommand{Type: "decide", ID: "1", Decision: "maybe"})

	message := client.readUntil(t, "error")
	if message.Error == "" {
		t.Fatal("error message has empty error field")
	}
}

func TestControlProtocolUnknownCommand(t *testing.T) {
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	path := startControlServer(t, coordinator)
	client := dialControl(t, path)
	client.read(t)

	client.send(t, controllerCommand{Type: "explode"})

	message := client.readUntil(t, "error")
	if message.Error == "" {
		t.Fatal("error message has empty error field")
	}
}
