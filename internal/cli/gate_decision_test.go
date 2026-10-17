package cli

import (
	"encoding/json"
	"net"
	"path/filepath"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/gate"
	tea "github.com/charmbracelet/bubbletea"
)

func TestGateDecisionKeysSendCorrectRequestScopeAndDuration(t *testing.T) {
	enter := tea.KeyMsg{Type: tea.KeyEnter}
	for _, tc := range []struct {
		name     string
		keys     []tea.KeyMsg
		decision string
		seconds  int
		scope    int
	}{
		{"allow", []tea.KeyMsg{runeKey('a')}, "allow", 0, 0},
		{"allow alias", []tea.KeyMsg{runeKey('y')}, "allow", 0, 0},
		{"deny", []tea.KeyMsg{runeKey('d')}, "deny", 0, 0},
		{"deny alias", []tea.KeyMsg{runeKey('n')}, "deny", 0, 0},
		{"remember preset", []tea.KeyMsg{runeKey('m'), enter}, "allow", 60, 2},
		{"remember custom", []tea.KeyMsg{runeKey('m'), runeKey('l'), runeKey('l'), runeKey('l'), enter, runeKey('2'), runeKey('0'), runeKey('s'), enter}, "allow", 20, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "controller.sock")
			listener, err := net.Listen("unix", path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = listener.Close() })
			type decision struct {
				ID              string `json:"id"`
				Decision        string `json:"decision"`
				RememberSeconds int    `json:"remember_seconds"`
				ScopeIndex      int    `json:"scope_index"`
			}
			decisions := make(chan decision, 1)
			errors := make(chan error, 1)
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					errors <- err
					return
				}
				defer func() { _ = conn.Close() }()
				if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
					errors <- err
					return
				}
				if err := json.NewEncoder(conn).Encode(map[string]any{
					"type": "hello", "requests": []gate.Request{scopeRequest("chosen")},
					"timeout_seconds": 60, "remember_default_seconds": 60,
				}); err != nil {
					errors <- err
					return
				}
				var command decision
				if err := json.NewDecoder(conn).Decode(&command); err != nil {
					errors <- err
					return
				}
				decisions <- command
			}()
			client, err := gate.DialController(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = client.Close() })
			model := newGateModel(client)
			model = pressGateKey(t, model, runeKey('h'))
			model = pressGateKey(t, model, runeKey('h'))
			for _, key := range tc.keys {
				updated, command := model.handleKey(key)
				model = updated.(gateModel)
				if command != nil {
					result := command().(decideResultMsg)
					if result.err != nil {
						t.Fatal(result.err)
					}
				}
			}
			select {
			case command := <-decisions:
				if command.ID != "chosen" || command.Decision != tc.decision || command.RememberSeconds != tc.seconds || command.ScopeIndex != tc.scope {
					t.Fatalf("sent decision = %+v, want %s/%ds/scope %d for chosen", command, tc.decision, tc.seconds, tc.scope)
				}
			case err := <-errors:
				t.Fatal(err)
			case <-time.After(5 * time.Second):
				t.Fatal("UI did not send a decision")
			}
		})
	}
}
