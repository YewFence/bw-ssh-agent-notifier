package gate

import (
	"errors"
	"testing"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
)

func TestAgentBuildRequestIncludesScopesAndNearestPreset(t *testing.T) {
	upstream, key := newTestKeyring(t)
	gateAgent := &Agent{
		Upstream: upstream,
		Client: process.Info{
			PID: 100, StartTime: 30, Exe: "/usr/bin/ssh", Cmdline: []string{"ssh", "git@example.test"},
			Parents: []process.Summary{
				{PID: 99, StartTime: 20, Exe: "/usr/bin/git", Cmdline: []string{"git", "fetch"}},
				{PID: 98, StartTime: 10, Exe: "/usr/bin/bd", Cmdline: []string{"bd", "sync"}},
				{PID: 97, StartTime: 5, Exe: "/usr/bin/zsh"},
			},
		},
		CommandGroups: [][]string{{"bd", "sync"}, {"git", "fetch"}},
	}
	request := gateAgent.buildRequest(key)
	if len(request.Scopes) != 3 || request.Scopes[2].PID != 98 || request.Scopes[2].StartTime != 10 {
		t.Fatalf("unexpected scopes: %+v", request.Scopes)
	}
	if request.DefaultScope != 1 || !request.ScopeAutomatic {
		t.Fatalf("nearest matching preset not selected: %+v", request)
	}
	if request.ClientPID != 100 || request.KeyComment != "test-key" || request.KeyFingerprint == "" {
		t.Fatalf("request metadata lost: %+v", request)
	}
	gateAgent.CommandGroups = nil
	request = gateAgent.buildRequest(key)
	if request.DefaultScope != 0 || request.ScopeAutomatic {
		t.Fatal("without groups the requester must be selected")
	}
}

func TestAgentRejectsChangedClientInstance(t *testing.T) {
	upstream, key := newTestKeyring(t)
	request := testRequest("unused")
	gateAgent := &Agent{
		Upstream: upstream,
		Client:   process.Info{PID: request.Scopes[0].PID, StartTime: request.Scopes[0].StartTime + 1},
	}
	if _, err := gateAgent.Sign(key, []byte("data")); !errors.Is(err, ErrRejected) {
		t.Fatalf("changed client instance error = %v, want rejected", err)
	}
}
