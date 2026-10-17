package cli

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/gate"
	"github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/spf13/cobra"
)

func newGateCommand(cfg *config.Config, envErr *error) *cobra.Command {
	return &cobra.Command{
		Use:   "gate",
		Short: "Approve or deny SSH signature requests interactively",
		RunE: func(cmd *cobra.Command, args []string) error {
			if *envErr != nil {
				return *envErr
			}
			return runGate(*cfg)
		},
	}
}

func runGate(cfg config.Config) error {
	if cfg.ControlSocket == "" {
		return fmt.Errorf("control socket is not set; set XDG_RUNTIME_DIR or WRAPPER_CONTROL_SOCKET")
	}
	client, err := gate.DialController(cfg.ControlSocket)
	if err != nil {
		return fmt.Errorf("%w; is the daemon running with --gate confirm?", err)
	}
	defer func() {
		_ = client.Close()
	}()

	program := tea.NewProgram(newGateModel(client))
	_, err = program.Run()
	return err
}

type controllerEventMsg gate.ControllerEvent

type controllerClosedMsg struct{}

type decideResultMsg struct{ err error }

type tickMsg time.Time

type gateModel struct {
	client          *gate.ControllerClient
	timeout         time.Duration
	rememberDefault time.Duration
	requests        []gate.Request
	selected        int
	flash           string
	width           int
	pickerActive    bool
	pickerIndex     int
	customActive    bool
	customInput     string
}

func newGateModel(client *gate.ControllerClient) gateModel {
	return gateModel{
		client:          client,
		timeout:         client.Timeout(),
		rememberDefault: client.RememberDefault(),
		requests:        client.Pending(),
	}
}

func (model gateModel) Init() tea.Cmd {
	return tea.Batch(waitControllerEvent(model.client), tick())
}

func (model gateModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch message := msg.(type) {
	case tea.WindowSizeMsg:
		model.width = message.Width
		return model, nil
	case controllerEventMsg:
		model.applyEvent(gate.ControllerEvent(message))
		return model, waitControllerEvent(model.client)
	case controllerClosedMsg:
		model.flash = "control connection closed"
		return model, tea.Quit
	case decideResultMsg:
		if message.err != nil {
			model.flash = fmt.Sprintf("decide failed: %v", message.err)
		}
		return model, nil
	case tickMsg:
		return model, tick()
	case tea.KeyMsg:
		return model.handleKey(message)
	}
	return model, nil
}

func (model *gateModel) applyEvent(event gate.ControllerEvent) {
	switch event.Type {
	case "pending":
		if event.Request != nil {
			model.requests = append(model.requests, *event.Request)
		}
	case "resolved":
		for i, request := range model.requests {
			if request.ID == event.ID {
				model.requests = append(model.requests[:i], model.requests[i+1:]...)
				model.flash = fmt.Sprintf("#%s %s (%s)", event.ID, event.Outcome, event.Source)
				break
			}
		}
		model.clampSelection()
	case "state":
		model.requests = event.Requests
		model.clampSelection()
	case "error":
		model.flash = event.Error
	}
}

func (model *gateModel) clampSelection() {
	if model.selected >= len(model.requests) {
		model.selected = max(len(model.requests)-1, 0)
	}
}

func (model gateModel) handleKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	if model.customActive {
		return model.handleCustomKey(message)
	}
	if model.pickerActive {
		return model.handlePickerKey(message)
	}
	switch message.String() {
	case "ctrl+c", "q":
		return model, tea.Quit
	case "up", "k":
		if model.selected > 0 {
			model.selected--
		}
		return model, nil
	case "down", "j":
		if model.selected < len(model.requests)-1 {
			model.selected++
		}
		return model, nil
	case "a", "y":
		if len(model.requests) == 0 {
			return model, nil
		}
		return model, decideCmd(model.client, model.requests[model.selected].ID, true, 0)
	case "d", "n":
		if len(model.requests) == 0 {
			return model, nil
		}
		return model, decideCmd(model.client, model.requests[model.selected].ID, false, 0)
	case "m":
		if len(model.requests) == 0 {
			return model, nil
		}
		if model.rememberDefault == 0 {
			model.flash = "remembering is disabled (--gate-remember-ttl=0)"
			return model, nil
		}
		model.pickerActive = true
		model.pickerIndex = defaultPickerIndex(model.rememberDefault)
		return model, nil
	}
	return model, nil
}

func (model gateModel) handlePickerKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "esc":
		model.pickerActive = false
		return model, nil
	case "up", "left", "k", "h":
		if model.pickerIndex > 0 {
			model.pickerIndex--
		}
		return model, nil
	case "down", "right", "j", "l":
		if model.pickerIndex < len(rememberPresets) {
			model.pickerIndex++
		}
		return model, nil
	case "enter":
		if model.pickerIndex < len(rememberPresets) {
			ttl := rememberPresets[model.pickerIndex]
			model.pickerActive = false
			return model, decideCmd(model.client, model.requests[model.selected].ID, true, ttl)
		}
		model.pickerActive = false
		model.customActive = true
		model.customInput = ""
		return model, nil
	}
	return model, nil
}

func (model gateModel) handleCustomKey(message tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch message.String() {
	case "esc":
		model.customActive = false
		model.customInput = ""
		return model, nil
	case "enter":
		ttl, err := parseRememberInput(model.customInput)
		if err != nil {
			model.flash = fmt.Sprintf("invalid duration %q; try 90s, 5m, or 2h", model.customInput)
			return model, nil
		}
		model.customActive = false
		model.customInput = ""
		return model, decideCmd(model.client, model.requests[model.selected].ID, true, ttl)
	case "backspace":
		if len(model.customInput) > 0 {
			model.customInput = model.customInput[:len(model.customInput)-1]
		}
		return model, nil
	}
	if len(message.String()) == 1 && isRememberInputRune(message.String()[0]) {
		model.customInput += message.String()
	}
	return model, nil
}

var rememberPresets = []time.Duration{time.Minute, 5 * time.Minute, 15 * time.Minute}

func defaultPickerIndex(defaultTTL time.Duration) int {
	for i, preset := range rememberPresets {
		if preset == defaultTTL {
			return i
		}
	}
	return 0
}

func isRememberInputRune(char byte) bool {
	return char == 's' || char == 'm' || char == 'h' || (char >= '0' && char <= '9')
}

func parseRememberInput(input string) (time.Duration, error) {
	if input == "" {
		return 0, fmt.Errorf("empty duration")
	}
	if duration, err := time.ParseDuration(input); err == nil {
		return duration, nil
	}
	minutes, err := strconv.Atoi(input)
	if err != nil {
		return 0, fmt.Errorf("invalid duration %q", input)
	}
	return time.Duration(minutes) * time.Minute, nil
}

func decideCmd(client *gate.ControllerClient, id string, allow bool, remember time.Duration) tea.Cmd {
	return func() tea.Msg {
		return decideResultMsg{err: client.Decide(id, allow, remember)}
	}
}

func waitControllerEvent(client *gate.ControllerClient) tea.Cmd {
	return func() tea.Msg {
		event, ok := <-client.Events()
		if !ok {
			return controllerClosedMsg{}
		}
		return controllerEventMsg(event)
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second, func(now time.Time) tea.Msg {
		return tickMsg(now)
	})
}

var (
	gateTitleStyle   = lipgloss.NewStyle().Bold(true)
	gateAllowStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "28", Dark: "2"}).Bold(true)
	gatePendingStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "94", Dark: "3"})
	gateDimStyle     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "241", Dark: "8"})
)

func (model gateModel) View() string {
	var builder strings.Builder
	builder.WriteString(gateTitleStyle.Render("bwsshntfr gate"))
	builder.WriteString(gateDimStyle.Render(fmt.Sprintf(" — SSH signature requests (timeout %s)\n\n", model.timeout)))
	if len(model.requests) == 0 {
		builder.WriteString(gateDimStyle.Render("no pending requests; waiting…"))
		builder.WriteString("\n")
	}
	for i, request := range model.requests {
		builder.WriteString(model.renderRequest(i, request))
		builder.WriteString("\n")
	}
	builder.WriteString("\n")
	if model.customActive {
		builder.WriteString(gateTitleStyle.Render(fmt.Sprintf("remember for: %s█  (enter confirm · esc cancel)", model.customInput)))
	} else if model.pickerActive {
		builder.WriteString(model.renderPicker())
	} else {
		builder.WriteString(gateDimStyle.Render("[a] allow  [m] allow+remember  [d] deny  [j/k] select  [q] quit"))
	}
	if model.flash != "" {
		builder.WriteString("\n")
		builder.WriteString(gatePendingStyle.Render(model.flash))
	}
	return builder.String()
}

func (model gateModel) renderPicker() string {
	labels := make([]string, 0, len(rememberPresets)+1)
	for i, preset := range rememberPresets {
		labels = append(labels, model.renderPickerChoice(preset.String(), i))
	}
	labels = append(labels, model.renderPickerChoice("custom", len(rememberPresets)))
	return gateTitleStyle.Render("remember for: ") + strings.Join(labels, "  ") + gateDimStyle.Render("  (enter confirm · esc cancel)")
}

func (model gateModel) renderPickerChoice(label string, index int) string {
	if index == model.pickerIndex {
		return gateAllowStyle.Render("▶ " + label)
	}
	return gateDimStyle.Render("  " + label)
}

func (model gateModel) renderRequest(index int, request gate.Request) string {
	keyStyle := gateTitleStyle
	cursor := "  "
	if index == model.selected {
		keyStyle = gateAllowStyle
		cursor = "▶ "
	}
	remaining := time.Until(request.Deadline).Round(time.Second)
	lines := []string{
		fmt.Sprintf("%s#%s  key: %s", cursor, request.ID, keyStyle.Render(request.KeyName())),
		fmt.Sprintf("     %s", request.ClientCmdline),
		fmt.Sprintf("     PID %d · %s", request.ClientPID, request.ClientExe),
	}
	if request.ParentChain != "" {
		lines = append(lines, fmt.Sprintf("     tree: %s", request.ParentChain))
	}
	lines = append(lines, fmt.Sprintf("     %s  fingerprint: %s", remaining, request.KeyFingerprint))
	for i := 1; i < len(lines); i++ {
		lines[i] = gateDimStyle.Render(lines[i])
	}
	return strings.Join(lines, "\n")
}
