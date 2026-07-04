package systemd

import (
	"strings"
	"testing"
)

func TestUserServiceReplacesPlaceholdersWithExplicitUpstream(t *testing.T) {
	service, err := UserService("/tmp/bwsshntfr", "/tmp/bitwarden.sock")
	if err != nil {
		t.Fatalf("UserService() error = %v", err)
	}
	if strings.Contains(service, "%h/.local/bin/bwsshntfr") {
		t.Fatalf("service still contains CLI placeholder:\n%s", service)
	}
	if strings.Contains(service, "# Environment=BITWARDEN_SSH_AGENT_SOCKET=") {
		t.Fatalf("service still contains commented upstream placeholder:\n%s", service)
	}
	if !strings.Contains(service, "ExecStart=/tmp/bwsshntfr") {
		t.Fatalf("service missing CLI path:\n%s", service)
	}
	if !strings.Contains(service, "Environment=BITWARDEN_SSH_AGENT_SOCKET=/tmp/bitwarden.sock") {
		t.Fatalf("service missing upstream path:\n%s", service)
	}
}

func TestUserServiceUsesSSHAuthSockWhenUpstreamIsEmpty(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/run/user/1000/ssh-agent.socket")

	service, err := UserService("/tmp/bwsshntfr", "")
	if err != nil {
		t.Fatalf("UserService() error = %v", err)
	}
	if !strings.Contains(service, "Environment=BITWARDEN_SSH_AGENT_SOCKET=/run/user/1000/ssh-agent.socket") {
		t.Fatalf("service missing SSH_AUTH_SOCK upstream path:\n%s", service)
	}
	if strings.Contains(service, "# Environment=BITWARDEN_SSH_AGENT_SOCKET=") {
		t.Fatalf("service still contains commented upstream placeholder:\n%s", service)
	}
}

func TestUserServiceKeepsCommentedUpstreamHintWithoutSocket(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")

	service, err := UserService("/tmp/bwsshntfr", "")
	if err != nil {
		t.Fatalf("UserService() error = %v", err)
	}
	if !strings.Contains(service, "# Environment=BITWARDEN_SSH_AGENT_SOCKET=$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock") {
		t.Fatalf("service missing commented upstream hint:\n%s", service)
	}
}
