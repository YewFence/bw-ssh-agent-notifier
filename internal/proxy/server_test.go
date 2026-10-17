package proxy

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func TestServerProxiesBytes(t *testing.T) {
	dir := t.TempDir()
	upstreamPath := filepath.Join(dir, "upstream.sock")
	listenPath := filepath.Join(dir, "listen.sock")

	upstream, err := net.ListenUnix("unix", &net.UnixAddr{Name: upstreamPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	defer func() {
		_ = upstream.Close()
	}()

	upstreamDone := make(chan error, 1)
	go func() {
		conn, err := upstream.AcceptUnix()
		if err != nil {
			upstreamDone <- err
			return
		}
		defer func() {
			_ = conn.Close()
		}()
		request, err := io.ReadAll(conn)
		if err != nil {
			upstreamDone <- err
			return
		}
		_, err = conn.Write(request)
		upstreamDone <- err
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	logBuffer := &bytes.Buffer{}
	server := Server{
		Config: config.Config{
			ListenSocket:        listenPath,
			UpstreamSocket:      upstreamPath,
			NotifyBackend:       "off",
			NotifyCallTimeout:   time.Second,
			NotifyExpireTimeout: time.Second,
			ParentDepth:         1,
			LogLevel:            config.DefaultLogLevel,
			LogFormat:           config.DefaultLogFormat,
			GateMode:            config.GateModeOff,
			GateTimeout:         config.DefaultGateTimeout,
		},
		Notifier: notify.Noop(),
		Logger:   slog.New(slog.NewTextHandler(logBuffer, nil)),
	}

	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx)
	}()
	waitForSocket(t, listenPath)

	client, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: listenPath, Net: "unix"})
	if err != nil {
		t.Fatalf("DialUnix() error = %v", err)
	}
	_, err = client.Write([]byte("ping"))
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatalf("CloseWrite() error = %v", err)
	}
	reply, err := io.ReadAll(client)
	if err != nil {
		t.Fatalf("ReadAll() error = %v", err)
	}
	if string(reply) != "ping" {
		t.Fatalf("reply = %q, want ping", reply)
	}
	if err := client.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	cancel()
	if err := <-serverDone; err != nil {
		t.Fatalf("server.Run() error = %v", err)
	}
	if err := <-upstreamDone; err != nil {
		t.Fatalf("upstream error = %v", err)
	}
}

func TestNotificationBodyIncludesCompactProcessTree(t *testing.T) {
	body := notificationBody(process.Info{
		PID: 42,
		Exe: "/usr/bin/ssh",
		Parents: []process.Summary{
			{PID: 41, Exe: "/usr/bin/git"},
			{PID: 40, Exe: "/usr/bin/zsh"},
			{PID: 39, Exe: "/usr/bin/ghostty"},
		},
	}, false)

	if !bytes.Contains([]byte(body), []byte("ssh is using Bitwarden SSH agent")) {
		t.Fatalf("notificationBody() = %q, want client process", body)
	}
	if !bytes.Contains([]byte(body), []byte("Process tree ssh <- git <- zsh")) {
		t.Fatalf("notificationBody() = %q, want process tree", body)
	}
	if bytes.Contains([]byte(body), []byte("ghostty")) {
		t.Fatalf("notificationBody() = %q, want compact process tree", body)
	}
}

func TestServerGateMode(t *testing.T) {
	dir := t.TempDir()
	upstreamPath := filepath.Join(dir, "upstream.sock")
	listenPath := filepath.Join(dir, "listen.sock")
	controlPath := filepath.Join(dir, "control.sock")

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

	upstreamListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: upstreamPath, Net: "unix"})
	if err != nil {
		t.Fatalf("ListenUnix() error = %v", err)
	}
	upstreamDone := make(chan struct{})
	go func() {
		conn, err := upstreamListener.AcceptUnix()
		if err != nil {
			close(upstreamDone)
			return
		}
		_ = agent.ServeAgent(keyring, conn)
		close(upstreamDone)
	}()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	server := Server{
		Config: config.Config{
			ListenSocket:        listenPath,
			UpstreamSocket:      upstreamPath,
			NotifyBackend:       "off",
			NotifyCallTimeout:   time.Second,
			NotifyExpireTimeout: time.Second,
			ParentDepth:         1,
			LogLevel:            config.DefaultLogLevel,
			LogFormat:           config.DefaultLogFormat,
			GateMode:            config.GateModeConfirm,
			GateTimeout:         100 * time.Millisecond,
			ControlSocket:       controlPath,
		},
		Notifier: notify.Noop(),
		Logger:   slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	serverDone := make(chan error, 1)
	go func() {
		serverDone <- server.Run(ctx)
	}()
	waitForSocket(t, listenPath)
	waitForSocket(t, controlPath)

	clientConn, err := net.Dial("unix", listenPath)
	if err != nil {
		t.Fatalf("Dial() error = %v", err)
	}
	defer func() {
		_ = clientConn.Close()
	}()
	client := agent.NewClient(clientConn)

	keys, err := client.List()
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("List() returned %d keys, want 1", len(keys))
	}

	if _, err := client.Sign(signer.PublicKey(), []byte("data")); err == nil {
		t.Fatal("Sign() error = nil, want denial without a gate controller")
	}

	cancel()
	if err := <-serverDone; err != nil {
		t.Fatalf("server.Run() error = %v", err)
	}
}

func TestNotificationBodyCanIncludeFullProcessTree(t *testing.T) {
	body := notificationBody(process.Info{
		PID: 42,
		Exe: "/usr/bin/ssh",
		Parents: []process.Summary{
			{PID: 41, Exe: "/usr/bin/git"},
			{PID: 40, Exe: "/usr/bin/zsh"},
			{PID: 39, Exe: "/usr/bin/ghostty"},
			{PID: 38, Exe: "/usr/lib/systemd/systemd"},
		},
	}, true)

	if !bytes.Contains([]byte(body), []byte("Process tree ssh <- git <- zsh <- ghostty <- systemd")) {
		t.Fatalf("notificationBody() = %q, want full process tree", body)
	}
}

func waitForSocket(t *testing.T, path string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		info, err := os.Stat(path)
		if err == nil && info.Mode()&os.ModeSocket != 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("socket %s was not created", path)
}
