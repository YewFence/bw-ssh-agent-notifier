package gate

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"time"

	"golang.org/x/sys/unix"
)

var errClientDisconnected = errors.New("client disconnected")

func watchClientDisconnect(ctx context.Context, conn *net.UnixConn, cancel context.CancelCauseFunc, logger *slog.Logger) {
	if logger == nil {
		logger = slog.Default()
	}
	raw, err := conn.SyscallConn()
	if err != nil {
		logger.Warn("monitor client connection failed", "error", err)
		return
	}
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		var pollErr error
		var events int16
		err := raw.Control(func(fd uintptr) {
			fds := []unix.PollFd{{Fd: int32(fd)}}
			_, pollErr = unix.Poll(fds, 0)
			events = fds[0].Revents
		})
		if errors.Is(pollErr, unix.EINTR) {
			pollErr = nil
		}
		if err != nil || pollErr != nil {
			logger.Warn("monitor client connection failed", "error", errors.Join(err, pollErr))
			return
		}
		// POLLHUP detects a full disconnect without consuming pipelined requests or treating a write-side shutdown as a disconnect.
		if events&(unix.POLLHUP|unix.POLLERR|unix.POLLNVAL) != 0 {
			cancel(errClientDisconnected)
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
