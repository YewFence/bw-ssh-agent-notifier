package gate

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"
)

const (
	messageHello    = "hello"
	messageState    = "state"
	messagePending  = "pending"
	messageResolved = "resolved"
	messageOK       = "ok"
	messageError    = "error"

	commandDecide = "decide"
	commandList   = "list"

	decisionAllow = "allow"
	decisionDeny  = "deny"
)

type serverMessage struct {
	Type                   string    `json:"type"`
	Requests               []Request `json:"requests,omitempty"`
	TimeoutSeconds         int       `json:"timeout_seconds,omitempty"`
	RememberDefaultSeconds int       `json:"remember_default_seconds,omitempty"`
	Request                *Request  `json:"request,omitempty"`
	ID                     string    `json:"id,omitempty"`
	Outcome                string    `json:"outcome,omitempty"`
	Source                 string    `json:"source,omitempty"`
	Error                  string    `json:"error,omitempty"`
}

type controllerCommand struct {
	Type            string `json:"type"`
	ID              string `json:"id,omitempty"`
	Decision        string `json:"decision,omitempty"`
	RememberSeconds int    `json:"remember_seconds,omitempty"`
}

type ControlServer struct {
	Coordinator *Coordinator
	Logger      *slog.Logger
}

func (controlServer *ControlServer) Serve(ctx context.Context, listener *net.UnixListener) error {
	logger := controlServer.logger()
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	for {
		conn, err := listener.AcceptUnix()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			logger.Warn("control accept failed", "error", err)
			continue
		}
		go controlServer.handleConn(ctx, conn)
	}
}

func (controlServer *ControlServer) handleConn(ctx context.Context, conn *net.UnixConn) {
	logger := controlServer.logger()
	defer func() {
		_ = conn.Close()
	}()
	controlServer.Coordinator.ControllerConnected()
	defer controlServer.Coordinator.ControllerDisconnected()

	events, cancelEvents := controlServer.Coordinator.Subscribe()
	defer cancelEvents()

	var writeMu sync.Mutex
	encoder := json.NewEncoder(conn)
	write := func(message serverMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(message)
	}

	hello := serverMessage{
		Type:                   messageHello,
		Requests:               controlServer.Coordinator.Pending(),
		TimeoutSeconds:         int(controlServer.Coordinator.Timeout().Seconds()),
		RememberDefaultSeconds: int(controlServer.Coordinator.RememberTTL().Seconds()),
	}
	if err := write(hello); err != nil {
		logger.Debug("control hello failed", "error", err)
		return
	}

	commands := make(chan controllerCommand, 4)
	go func() {
		defer close(commands)
		scanner := bufio.NewScanner(conn)
		for scanner.Scan() {
			var command controllerCommand
			if err := json.Unmarshal(scanner.Bytes(), &command); err != nil {
				logger.Warn("control command parse failed", "error", err)
				continue
			}
			select {
			case commands <- command:
			case <-ctx.Done():
				return
			}
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case command, ok := <-commands:
			if !ok {
				return
			}
			controlServer.handleCommand(command, write, logger)
		case event := <-events:
			if err := write(eventMessage(event)); err != nil {
				logger.Debug("control write failed", "error", err)
				return
			}
		}
	}
}

func (controlServer *ControlServer) handleCommand(command controllerCommand, write func(serverMessage) error, logger *slog.Logger) {
	switch command.Type {
	case commandList:
		if err := write(serverMessage{Type: messageState, Requests: controlServer.Coordinator.Pending()}); err != nil {
			logger.Debug("control write failed", "error", err)
		}
	case commandDecide:
		if command.Decision != decisionAllow && command.Decision != decisionDeny {
			_ = write(serverMessage{Type: messageError, ID: command.ID, Error: fmt.Sprintf("invalid decision %q", command.Decision)})
			return
		}
		remember := time.Duration(command.RememberSeconds) * time.Second
		if err := controlServer.Coordinator.Decide(command.ID, command.Decision == decisionAllow, remember); err != nil {
			_ = write(serverMessage{Type: messageError, ID: command.ID, Error: err.Error()})
			return
		}
		_ = write(serverMessage{Type: messageOK, ID: command.ID})
	default:
		_ = write(serverMessage{Type: messageError, Error: fmt.Sprintf("unknown command type %q", command.Type)})
	}
}

func eventMessage(event Event) serverMessage {
	if event.Kind == EventPending {
		return serverMessage{Type: messagePending, Request: &event.Request}
	}
	return serverMessage{
		Type:    messageResolved,
		ID:      event.ID,
		Outcome: string(event.Result.Outcome),
		Source:  string(event.Result.Source),
	}
}

func (controlServer *ControlServer) logger() *slog.Logger {
	if controlServer.Logger != nil {
		return controlServer.Logger
	}
	return slog.Default()
}
