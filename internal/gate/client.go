package gate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"time"
)

// ControllerEvent is one control protocol message surfaced to a controller
// UI, such as the bwsshntfr gate TUI.
type ControllerEvent struct {
	Type     string
	Request  *Request
	Requests []Request
	ID       string
	Outcome  string
	Source   string
	Error    string
}

type ControllerClient struct {
	conn    net.Conn
	encoder *json.Encoder
	writeMu sync.Mutex
	events  chan ControllerEvent
	hello   serverMessage
}

func DialController(path string) (*ControllerClient, error) {
	conn, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("connect control socket %s: %w", path, err)
	}
	client := &ControllerClient{
		conn:    conn,
		encoder: json.NewEncoder(conn),
		events:  make(chan ControllerEvent, eventChannelSize),
	}

	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("read control hello: %w", err)
	}
	if err := json.Unmarshal([]byte(line), &client.hello); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("parse control hello: %w", err)
	}
	if client.hello.Type != messageHello {
		_ = conn.Close()
		return nil, fmt.Errorf("unexpected first control message %q", client.hello.Type)
	}

	go client.readLoop(reader)
	return client, nil
}

func (client *ControllerClient) Pending() []Request {
	return client.hello.Requests
}

func (client *ControllerClient) Timeout() time.Duration {
	return time.Duration(client.hello.TimeoutSeconds) * time.Second
}

func (client *ControllerClient) RememberDefault() time.Duration {
	return time.Duration(client.hello.RememberDefaultSeconds) * time.Second
}

func (client *ControllerClient) Events() <-chan ControllerEvent {
	return client.events
}

func (client *ControllerClient) Decide(id string, allow bool, remember time.Duration, scopeIndex int) error {
	decision := decisionAllow
	if !allow {
		decision = decisionDeny
	}
	client.writeMu.Lock()
	defer client.writeMu.Unlock()
	return client.encoder.Encode(controllerCommand{
		Type:            commandDecide,
		ID:              id,
		Decision:        decision,
		RememberSeconds: int(remember.Seconds()),
		ScopeIndex:      scopeIndex,
	})
}

func (client *ControllerClient) Close() error {
	return client.conn.Close()
}

func (client *ControllerClient) readLoop(reader *bufio.Reader) {
	defer close(client.events)
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		var message serverMessage
		if err := json.Unmarshal(scanner.Bytes(), &message); err != nil {
			continue
		}
		client.events <- ControllerEvent{
			Type:     message.Type,
			Request:  message.Request,
			Requests: message.Requests,
			ID:       message.ID,
			Outcome:  message.Outcome,
			Source:   message.Source,
			Error:    message.Error,
		}
	}
}
