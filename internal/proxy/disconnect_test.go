package proxy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/gate"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func startDisconnectTestServer(t *testing.T) (string, *gate.ControllerClient, ssh.PublicKey) {
	t.Helper()
	dir := t.TempDir()
	upstreamPath := filepath.Join(dir, "upstream.sock")
	listenPath := filepath.Join(dir, "wrapper.sock")
	controlPath := filepath.Join(dir, "control.sock")
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring().(agent.ExtendedAgent)
	if err := keyring.Add(agent.AddedKey{PrivateKey: privateKey}); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := net.ListenUnix("unix", &net.UnixAddr{Name: upstreamPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upstream.Close() })
	go func() {
		for {
			conn, err := upstream.AcceptUnix()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	server := Server{
		Config: config.Config{
			ListenSocket: listenPath, UpstreamSocket: upstreamPath, ControlSocket: controlPath,
			GateMode: config.GateModeConfirm, GateTimeout: 10 * time.Second, GateRememberTTL: time.Minute,
			ParentDepth: 1, NotifyBackend: "off", NotifyCallTimeout: time.Second, NotifyExpireTimeout: time.Second,
			LogLevel: config.DefaultLogLevel, LogFormat: config.DefaultLogFormat,
		},
		Notifier: notify.Noop(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	done := make(chan error, 1)
	go func() { done <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("server.Run(): %v", err)
		}
	})
	waitForSocket(t, listenPath)
	waitForSocket(t, controlPath)
	controller, err := gate.DialController(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controller.Close() })
	return listenPath, controller, signer.PublicKey()
}

func startPendingSignature(t *testing.T, path string, controller *gate.ControllerClient, key ssh.PublicKey) (*net.UnixConn, gate.Request, <-chan error) {
	t.Helper()
	conn, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	done := make(chan error, 1)
	go func() {
		payload := []byte("disconnect integration test")
		signature, err := agent.NewClient(conn).Sign(key, payload)
		if err == nil {
			err = key.Verify(payload, signature)
		}
		done <- err
	}()
	select {
	case event := <-controller.Events():
		if event.Type != "pending" || event.Request == nil {
			t.Fatalf("expected pending request, got %+v", event)
		}
		return conn, *event.Request, done
	case <-time.After(2 * time.Second):
		t.Fatal("signature request was not submitted")
		return nil, gate.Request{}, nil
	}
}

func TestGateDisconnectCancelsOnlyItsPendingRequest(t *testing.T) {
	path, controller, key := startDisconnectTestServer(t)
	firstConn, first, firstDone := startPendingSignature(t, path, controller, key)
	_, second, secondDone := startPendingSignature(t, path, controller, key)
	if _, err := firstConn.Write([]byte{0, 0, 0, 1, 11}); err != nil {
		t.Fatal(err)
	}
	if err := firstConn.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-controller.Events():
		if event.Type != "resolved" || event.ID != first.ID || event.Outcome != "denied" || event.Source != "client-disconnected" {
			t.Fatalf("disconnect event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disconnected request remained pending")
	}
	if err := <-firstDone; err == nil {
		t.Fatal("disconnected client unexpectedly received a signature")
	}
	select {
	case err := <-secondDone:
		t.Fatalf("unrelated request resolved early: %v", err)
	default:
	}
	if err := controller.Decide(second.ID, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-secondDone:
		if err != nil {
			t.Fatalf("allow unrelated request: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("unrelated request did not complete")
	}
}

func TestGateRequesterExitCancelsPendingRequest(t *testing.T) {
	path, controller, key := startDisconnectTestServer(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestGateSignatureHelper$")
	command.Env = append(os.Environ(),
		"BWSSHNTFR_TEST_SIGN=1",
		"BWSSHNTFR_TEST_SOCKET="+path,
		"BWSSHNTFR_TEST_PUBLIC_KEY="+string(ssh.MarshalAuthorizedKey(key)),
	)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if command.ProcessState == nil {
			_ = command.Process.Kill()
			_ = command.Wait()
		}
	})
	var request gate.Request
	select {
	case event := <-controller.Events():
		if event.Type != "pending" || event.Request == nil {
			t.Fatalf("expected pending request, got %+v", event)
		}
		request = *event.Request
	case <-time.After(2 * time.Second):
		t.Fatal("child did not submit a signature request")
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	select {
	case event := <-controller.Events():
		if event.Type != "resolved" || event.ID != request.ID || event.Source != "client-disconnected" {
			t.Fatalf("requester exit event = %+v", event)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("request remained pending after requester exited")
	}
}

func TestGateClientHalfCloseStillReceivesSignature(t *testing.T) {
	path, controller, key := startDisconnectTestServer(t)
	conn, request, done := startPendingSignature(t, path, controller, key)
	if err := conn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case event := <-controller.Events():
		t.Fatalf("half-closed request unexpectedly resolved: %+v", event)
	case <-time.After(500 * time.Millisecond):
	}
	if err := controller.Decide(request.ID, true, 0, 0); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("allow half-closed request: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("half-closed request did not complete")
	}
}
