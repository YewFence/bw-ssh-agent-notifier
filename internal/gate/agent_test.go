package gate

import (
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func newTestKeyring(t *testing.T) (agent.ExtendedAgent, ssh.PublicKey) {
	t.Helper()
	keyring := agent.NewKeyring().(agent.ExtendedAgent)
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if err := keyring.Add(agent.AddedKey{PrivateKey: privateKey, Comment: "test-key"}); err != nil {
		t.Fatalf("Add() error = %v", err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatalf("NewSignerFromKey() error = %v", err)
	}
	return keyring, signer.PublicKey()
}

func newGateClient(t *testing.T, coordinator *Coordinator, upstream agent.ExtendedAgent) agent.ExtendedAgent {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	gateAgent := &Agent{
		ConnCtx:     t.Context(),
		Upstream:    upstream,
		Coordinator: coordinator,
		Client: process.Info{
			PID:     4242,
			Exe:     "/usr/bin/ssh",
			Cmdline: []string{"ssh", "git@github.com"},
		},
		Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	go func() {
		_ = agent.ServeAgent(gateAgent, serverConn)
	}()
	return agent.NewClient(clientConn)
}

func autoAllow(t *testing.T, coordinator *Coordinator) {
	t.Helper()
	events, cancel := coordinator.Subscribe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for event := range events {
			if event.Kind == EventPending {
				if err := coordinator.Decide(event.Request.ID, true, 0); err != nil {
					return
				}
			}
		}
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
}

func TestAgentListPassesThrough(t *testing.T) {
	keyring, _ := newTestKeyring(t)
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	client := newGateClient(t, coordinator, keyring)

	keys, err := client.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("List() returned %d keys, want 1", len(keys))
	}
	if keys[0].Comment != "test-key" {
		t.Fatalf("List() comment = %q, want test-key", keys[0].Comment)
	}
}

func TestAgentSignAllowedByController(t *testing.T) {
	keyring, publicKey := newTestKeyring(t)
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	coordinator.ControllerConnected()
	autoAllow(t, coordinator)
	client := newGateClient(t, coordinator, keyring)

	data := []byte("test data")
	signature, err := client.Sign(publicKey, data)
	if err != nil {
		t.Fatalf("Sign() error = %v", err)
	}
	if err := publicKey.Verify(data, signature); err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
}

func TestAgentSignDeniedWithoutController(t *testing.T) {
	keyring, publicKey := newTestKeyring(t)
	coordinator := newTestCoordinator(100*time.Millisecond, notify.Noop())
	client := newGateClient(t, coordinator, keyring)

	if _, err := client.Sign(publicKey, []byte("test data")); err == nil {
		t.Fatal("Sign() error = nil, want denial without controller")
	}
}

func TestAgentManagementRejected(t *testing.T) {
	keyring, publicKey := newTestKeyring(t)
	coordinator := newTestCoordinator(time.Minute, notify.Noop())
	client := newGateClient(t, coordinator, keyring)

	if err := client.RemoveAll(); err == nil {
		t.Error("RemoveAll() error = nil, want rejection")
	}
	if err := client.Remove(publicKey); err == nil {
		t.Error("Remove() error = nil, want rejection")
	}
	if err := client.Lock([]byte("passphrase")); err == nil {
		t.Error("Lock() error = nil, want rejection")
	}
	if _, err := client.Extension("session-bind@openssh.com", []byte("payload")); err == nil {
		t.Error("Extension() error = nil, want rejection")
	}

	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	if err := client.Add(agent.AddedKey{PrivateKey: privateKey}); err == nil {
		t.Error("Add() error = nil, want rejection")
	}
}
