package proxy

import (
	"bytes"
	"context"
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
