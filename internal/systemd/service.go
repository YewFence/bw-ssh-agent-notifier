package systemd

import (
	_ "embed"
	"fmt"
	"os"
	"strings"
)

const (
	cliPathPlaceholder        = "%h/.local/bin/bwsshntfr"
	upstreamSocketPlaceholder = "# Environment=BITWARDEN_SSH_AGENT_SOCKET=$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock"
)

//go:embed bitwarden-ssh-agent-wrapper.service
var userServiceTemplate string

func CurrentExecutablePath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", err
	}
	if path == "" {
		return "", fmt.Errorf("executable path is empty")
	}
	return path, nil
}

func UserService(cliPath, upstreamSocket string) (string, error) {
	if cliPath == "" {
		return "", fmt.Errorf("CLI path is required")
	}
	if upstreamSocket == "" {
		upstreamSocket = os.Getenv("SSH_AUTH_SOCK")
	}
	service := strings.ReplaceAll(userServiceTemplate, cliPathPlaceholder, cliPath)
	if upstreamSocket != "" {
		service = strings.ReplaceAll(service, upstreamSocketPlaceholder, "Environment=BITWARDEN_SSH_AGENT_SOCKET="+upstreamSocket)
	}
	return service, nil
}
