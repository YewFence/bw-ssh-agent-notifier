package cli

import (
	"strings"
	"testing"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/gate"
)

func TestGateViewRequestHeaderStartsAtLeftEdge(t *testing.T) {
	request := scopeRequest("alignment")
	for _, width := range []int{0, 48, 120} {
		model := gateModel{requests: []gate.Request{request}, width: width}
		found := false
		for _, line := range strings.Split(model.View(), "\n") {
			if !strings.HasPrefix(line, "▶ #alignment  key: Laptop  timeout:") {
				continue
			}
			found = true
			if strings.TrimLeft(line, " ") != line {
				t.Fatalf("width %d: request header is not left-aligned: %q", width, line)
			}
		}
		if !found {
			t.Fatalf("width %d: missing request header:\n%s", width, model.View())
		}
		if !strings.Contains(model.renderRequest(0, request), "key: Laptop  timeout: 1m0s") {
			t.Fatalf("width %d: timeout is not prominent in request title:\n%s", width, model.renderRequest(0, request))
		}
	}
}

func TestGateFieldWrapsValuesUnderTheValueColumn(t *testing.T) {
	model := gateModel{width: 48}
	rendered := model.renderField("requester:", "ssh "+strings.Repeat("Z", 120))
	lines := strings.Split(rendered, "\n")
	if len(lines) < 2 || !strings.HasPrefix(lines[0], "  requester:      ssh ") {
		t.Fatalf("expected requester field to wrap: %q", rendered)
	}
	for _, line := range lines[1:] {
		if !strings.HasPrefix(line, strings.Repeat(" ", 18)) {
			t.Fatalf("continuation is not aligned with value column: %q", line)
		}
	}
	if strings.Count(rendered, "Z") != 120 {
		t.Fatal("wrapped value lost argument text")
	}
}

func TestGateRequestFieldsHaveAlignedLabelsAndValues(t *testing.T) {
	request := scopeRequest("fields")
	request.ClientExe = "/usr/bin/ssh"
	request.KeyFingerprint = "SHA256:test-fingerprint"
	request.ParentChain = "ssh <- git <- bd <- zsh"
	model := gateModel{scopeSelections: map[string]int{request.ID: 2}}
	lines := strings.Split(model.renderRequest(0, request), "\n")
	want := []struct{ label, value string }{
		{"requester:", "ssh git@example.test"},
		{"pid:", "100"},
		{"executable:", "/usr/bin/ssh"},
		{"tree:", "ssh <- git <- bd <- zsh"},
		{"fingerprint:", "SHA256:test-fingerprint"},
		{"commands:", "[bd] > git > ssh"},
		{"remember scope:", "bd sync"},
		{"scope pid:", "98"},
		{"scope exe:", "/usr/bin/bd"},
		{"selection:", "manual selection"},
	}
	for _, field := range want {
		found := false
		for _, line := range lines {
			if strings.HasPrefix(line, "  "+field.label) {
				found = true
				if strings.Index(line, field.value) != 18 || strings.TrimSpace(line[18:]) != field.value {
					t.Fatalf("field %s has incorrect value or value column: %q", field.label, line)
				}
				break
			}
		}
		if !found {
			t.Fatalf("missing field %s:\n%s", field.label, strings.Join(lines, "\n"))
		}
	}
}
