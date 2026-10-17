package cli

import (
	"strings"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/gate"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func scopeRequest(id string) gate.Request {
	return gate.Request{
		ID: id, KeyComment: "Laptop", ClientPID: 100, ClientCmdline: "ssh git@example.test",
		Deadline: time.Now().Add(time.Minute),
		Scopes: []process.Summary{
			{PID: 100, StartTime: 30, Exe: "/usr/bin/ssh", Cmdline: []string{"ssh", "git@example.test"}},
			{PID: 99, StartTime: 20, Exe: "/usr/bin/git", Cmdline: []string{"git", "fetch", "origin"}},
			{PID: 98, StartTime: 10, Exe: "/usr/bin/bd", Cmdline: []string{"bd", "sync"}},
		},
	}
}

func pressGateKey(t *testing.T, model gateModel, key tea.KeyMsg) gateModel {
	t.Helper()
	updated, command := model.handleKey(key)
	if command != nil {
		t.Fatal("navigation must not submit an authorization")
	}
	return updated.(gateModel)
}

func runeKey(char rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{char}}
}

func TestGateScopeNavigationNeedsNoConfirmation(t *testing.T) {
	request := scopeRequest("one")
	model := gateModel{requests: []gate.Request{request}}
	if got := model.selectedScope(request); got != 0 {
		t.Fatalf("default scope = %d, want requester", got)
	}
	for _, step := range []struct {
		key  tea.KeyMsg
		want int
	}{
		{runeKey('h'), 1}, {tea.KeyMsg{Type: tea.KeyLeft}, 2},
		{runeKey('h'), 2}, {runeKey('l'), 1},
		{tea.KeyMsg{Type: tea.KeyRight}, 0}, {runeKey('l'), 0},
	} {
		model = pressGateKey(t, model, step.key)
		if got := model.selectedScope(request); got != step.want {
			t.Fatalf("scope after %s = %d, want %d", step.key.String(), got, step.want)
		}
	}
}

func TestGateScopeSelectionSurvivesRequestNavigationAndRefresh(t *testing.T) {
	first, second := scopeRequest("first"), scopeRequest("second")
	model := gateModel{requests: []gate.Request{first, second}}
	model = pressGateKey(t, model, runeKey('h'))
	model = pressGateKey(t, model, runeKey('j'))
	model = pressGateKey(t, model, runeKey('h'))
	model = pressGateKey(t, model, runeKey('h'))
	model = pressGateKey(t, model, runeKey('k'))
	if model.selectedScope(first) != 1 || model.selectedScope(second) != 2 {
		t.Fatalf("per-request selections lost: %+v", model.scopeSelections)
	}
	first.DefaultScope, second.DefaultScope = 0, 0
	model.applyEvent(gate.ControllerEvent{Type: "state", Requests: []gate.Request{second, first}})
	if model.selected != 1 || model.selectedScope(first) != 1 || model.selectedScope(second) != 2 {
		t.Fatalf("refresh lost selection: %+v", model)
	}
	model.applyEvent(gate.ControllerEvent{Type: "resolved", ID: "first"})
	if _, exists := model.scopeSelections["first"]; exists {
		t.Fatal("resolved request selection was not removed")
	}
}

func TestGatePresetCanBeOverridden(t *testing.T) {
	request := scopeRequest("preset")
	request.DefaultScope = 2
	request.ScopeAutomatic = true
	model := gateModel{requests: []gate.Request{request}}
	if model.selectedScope(request) != 2 {
		t.Fatal("preset was not selected")
	}
	if rendered := model.renderRequest(0, request); !strings.Contains(rendered, "automatic: command group") {
		t.Fatalf("missing automatic selection explanation: %s", rendered)
	}
	model = pressGateKey(t, model, runeKey('l'))
	model.applyEvent(gate.ControllerEvent{Type: "state", Requests: []gate.Request{request}})
	if model.selectedScope(request) != 1 {
		t.Fatal("refresh overrode manual selection")
	}
	if rendered := model.renderRequest(0, request); !strings.Contains(rendered, "manual selection") {
		t.Fatalf("missing manual selection explanation: %s", rendered)
	}
}

func TestGateRememberPickerKeepsScopeAndExistingKeys(t *testing.T) {
	request := scopeRequest("picker")
	model := gateModel{requests: []gate.Request{request}, rememberDefault: time.Minute}
	model = pressGateKey(t, model, runeKey('h'))
	model = pressGateKey(t, model, runeKey('m'))
	if !model.pickerActive || model.pickerRequestID != request.ID || model.pickerScope != 1 {
		t.Fatalf("picker did not capture selected scope: %+v", model)
	}
	model = pressGateKey(t, model, runeKey('l'))
	if model.pickerIndex != 1 || model.selectedScope(request) != 1 {
		t.Fatal("picker navigation changed command scope instead of duration")
	}
	model.applyEvent(gate.ControllerEvent{Type: "resolved", ID: request.ID})
	if model.pickerActive || model.customActive {
		t.Fatal("picker for resolved request remained active")
	}
}

func TestGateRenderExplainsRequesterScopeAndLifetime(t *testing.T) {
	request := scopeRequest("render")
	model := gateModel{scopeSelections: map[string]int{request.ID: 2}}
	rendered := model.renderRequest(0, request)
	for _, text := range []string{
		"  requester:      ssh git@example.test", "  remember scope: bd sync", "  scope pid:      98",
		"  selection:      manual selection", "  commands:       [bd] > git > ssh",
		"timeout:",
	} {
		if !strings.Contains(rendered, text) {
			t.Fatalf("rendered request missing %q:\n%s", text, rendered)
		}
	}
}

func TestGateRenderEmphasizesRememberScopeValue(t *testing.T) {
	request := scopeRequest("emphasis")
	model := gateModel{scopeSelections: map[string]int{request.ID: 2}}
	rendered := model.renderRequest(0, request)
	if !strings.Contains(rendered, "remember scope:") || !strings.Contains(rendered, "bd sync") {
		t.Fatalf("rendered request missing remember scope: %q", rendered)
	}
	if !strings.Contains(rendered, gateTitleStyle.Render("bd sync")) {
		t.Fatalf("remember scope value is not emphasized: %q", rendered)
	}
	if strings.Contains(rendered, "access:") || strings.Contains(rendered, "lifetime:") {
		t.Fatalf("rendered request contains explanatory policy fields: %q", rendered)
	}
}
func TestGateViewWrapsLongCommandsWithoutHidingArguments(t *testing.T) {
	request := scopeRequest("wrap")
	request.Scopes[0].Cmdline = []string{"ssh", "--long-argument=" + strings.Repeat("Z", 120)}
	model := gateModel{requests: []gate.Request{request}, width: 48}
	rendered := model.View()
	for _, line := range strings.Split(rendered, "\n") {
		if width := lipgloss.Width(line); width > model.width {
			t.Fatalf("rendered line width = %d, want at most %d: %q", width, model.width, line)
		}
	}
	if strings.Count(rendered, "Z") != 120 {
		t.Fatal("wrapped command lost argument text")
	}
}

func TestGateViewEscapesTerminalControlSequences(t *testing.T) {
	request := scopeRequest("escape")
	request.KeyComment = "Laptop\x1b[2J\nforged"
	request.ClientCmdline = "ssh\x1b[2J\nforged"
	request.Scopes[0].Cmdline = []string{"ssh", "\x1b[2J\nforged"}
	rendered := (gateModel{}).renderRequest(0, request)
	if strings.Contains(rendered, "\x1b[2J") || strings.Contains(rendered, "\nforged") {
		t.Fatalf("terminal controls were not escaped: %q", rendered)
	}
}

func TestGateMissingProcessIdentityCannotBeRemembered(t *testing.T) {
	request := scopeRequest("unknown")
	request.Scopes = nil
	model := gateModel{requests: []gate.Request{request}, rememberDefault: time.Minute}
	model = pressGateKey(t, model, runeKey('h'))
	model = pressGateKey(t, model, runeKey('m'))
	if model.pickerActive || !strings.Contains(model.flash, "could not be identified") {
		t.Fatal("missing process identity should disable remembering")
	}
}
