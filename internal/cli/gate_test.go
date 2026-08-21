package cli

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

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
