package cli

import (
	"strings"
	"testing"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/gate"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func TestRenderRequestPrioritizesReadableKeyName(t *testing.T) {
	request := gate.Request{
		ID:             "7",
		KeyComment:     "GitSigning",
		KeyFingerprint: "SHA256:less-important",
		ClientCmdline:  "git push origin main",
		ClientPID:      1234,
		ClientExe:      "/usr/bin/git",
	}

	rendered := (gateModel{}).renderRequest(0, request)
	nameIndex := strings.Index(rendered, "key: GitSigning")
	fingerprintIndex := strings.Index(rendered, "fingerprint: SHA256:less-important")
	if nameIndex < 0 {
		t.Fatalf("renderRequest() = %q, want readable key name", rendered)
	}
	if fingerprintIndex < 0 {
		t.Fatalf("renderRequest() = %q, want fingerprint as secondary detail", rendered)
	}
	if nameIndex > fingerprintIndex {
		t.Fatalf("renderRequest() = %q, want key name before fingerprint", rendered)
	}
}

func TestGateStylesAdaptToTerminalBackground(t *testing.T) {
	renderer := lipgloss.DefaultRenderer()
	previousProfile := renderer.ColorProfile()
	t.Cleanup(func() { renderer.SetColorProfile(previousProfile) })

	renderer.SetColorProfile(termenv.ANSI256)

	styles := map[string]lipgloss.Style{
		"gateAllowStyle":   gateAllowStyle,
		"gatePendingStyle": gatePendingStyle,
		"gateDimStyle":     gateDimStyle,
	}
	for name, style := range styles {
		t.Run(name, func(t *testing.T) {
			renderer.SetHasDarkBackground(false)
			light := style.Render("text")
			renderer.SetHasDarkBackground(true)
			dark := style.Render("text")

			if light == dark {
				t.Fatalf("style %s rendered identically on light and dark backgrounds: %q", name, light)
			}
			if light == "text" {
				t.Fatalf("style %s rendered without color on a light background", name)
			}
		})
	}
}
