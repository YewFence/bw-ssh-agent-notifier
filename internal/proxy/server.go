package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
)

type Server struct {
	Config   config.Config
	Notifier notify.Notifier
	Logger   *slog.Logger
}

func (server Server) Run(ctx context.Context) error {
	if err := server.Config.Validate(); err != nil {
		return err
	}
	if _, err := os.Stat(server.Config.UpstreamSocket); err != nil {
		return fmt.Errorf("upstream socket unavailable: %w", err)
	}

	logger := server.Logger
	if logger == nil {
		logger = slog.Default()
	}
	notifier := server.Notifier
	if notifier == nil {
		notifier = notify.Noop()
	}

	if err := prepareSocket(server.Config.ListenSocket); err != nil {
		return err
	}

	addr := net.UnixAddr{Name: server.Config.ListenSocket, Net: "unix"}
	listener, err := net.ListenUnix("unix", &addr)
	if err != nil {
		return err
	}
	defer func() {
		if err := listener.Close(); err != nil {
			logger.Warn("close listener failed", "error", err)
		}
		if err := os.Remove(server.Config.ListenSocket); err != nil && !errors.Is(err, os.ErrNotExist) {
			logger.Warn("remove listen socket failed", "path", server.Config.ListenSocket, "error", err)
		}
	}()

	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	logger.Info("listening", "listen_socket", server.Config.ListenSocket, "upstream_socket", server.Config.UpstreamSocket)
	for {
		client, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			logger.Warn("accept failed", "error", err)
			continue
		}
		go server.handleClient(ctx, client, notifier, logger)
	}
}

func (server Server) handleClient(ctx context.Context, client *net.UnixConn, notifier notify.Notifier, logger *slog.Logger) {
	defer func() {
		_ = client.Close()
	}()

	cred, err := process.PeerCredentials(client)
	if err != nil {
		logger.Warn("read peer credentials failed", "error", err)
	} else {
		server.logClient(ctx, notifier, logger, cred)
	}

	upstream, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: server.Config.UpstreamSocket, Net: "unix"})
	if err != nil {
		logger.Error("connect upstream failed", "upstream_socket", server.Config.UpstreamSocket, "error", err)
		return
	}
	defer func() {
		_ = upstream.Close()
	}()

	if err := proxyConnections(client, upstream); err != nil {
		logger.Debug("proxy connection closed", "error", err)
	}
}

func (server Server) logClient(ctx context.Context, notifier notify.Notifier, logger *slog.Logger, cred process.Credentials) {
	info, err := process.Inspect(cred.PID, server.Config.ParentDepth)
	attrs := []any{
		"client_pid", cred.PID,
		"client_uid", cred.UID,
		"client_gid", cred.GID,
		"upstream_socket", server.Config.UpstreamSocket,
	}
	if err != nil {
		attrs = append(attrs, "inspect_error", err)
	}

	cmdline := process.CommandLine(info.Cmdline)
	parentChain := process.ParentChain(info.Parents)
	attrs = append(attrs,
		"client_exe", info.Exe,
		"client_cmdline", cmdline,
		"parent_chain", parentChain,
	)
	if err != nil {
		logger.Warn("ssh agent client connected", attrs...)
	} else {
		logger.Info("ssh agent client connected", attrs...)
	}

	if server.Config.NotifyBackend == "off" {
		return
	}
	body := notificationBody(info, server.Config.NotifyFullTree)
	if err := notifier.Send(ctx, notify.Notification{Summary: "SSH agent request", Body: body}); err != nil {
		logger.Warn("send notification failed", "error", err)
	}
}

func notificationBody(info process.Info, fullTree bool) string {
	client := process.Summary{PID: info.PID, Exe: info.Exe, Cmdline: info.Cmdline}
	clientName := process.ProcessName(client)
	body := fmt.Sprintf("%s is using Bitwarden SSH agent\nPID %d", clientName, info.PID)
	if info.Exe != "" {
		body = fmt.Sprintf("%s · %s", body, info.Exe)
	}
	chain := process.CompactProcessChain(client, info.Parents)
	if fullTree {
		chain = process.ProcessChain(client, info.Parents)
	}
	if chain != clientName {
		body = fmt.Sprintf("%s\nProcess tree %s", body, chain)
	}
	return body
}

func prepareSocket(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}

	conn, err := net.DialTimeout("unix", path, 200*time.Millisecond)
	if err == nil {
		_ = conn.Close()
		return fmt.Errorf("listen socket already has an active server: %s", path)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if _, statErr := os.Stat(path); statErr == nil {
		return os.Remove(path)
	}
	return nil
}

func proxyConnections(client, upstream *net.UnixConn) error {
	errs := make(chan error, 2)
	go copyAndClose(upstream, client, errs)
	go copyAndClose(client, upstream, errs)

	firstErr := <-errs
	secondErr := <-errs
	if firstErr != nil {
		return firstErr
	}
	return secondErr
}

func copyAndClose(dst, src *net.UnixConn, errs chan<- error) {
	_, err := io.Copy(dst, src)
	_ = dst.CloseWrite()
	errs <- err
}
