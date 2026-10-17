package gate

import (
	"reflect"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
)

func TestControllerSelectsAncestorScopeAndRemembersSibling(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	owner := testRequest("SHA256:wire").Scopes[0]
	request := testRequest("SHA256:wire")
	request.ClientPID = 4242
	request.Scopes = []process.Summary{{PID: 4242, StartTime: 123, Exe: "/usr/bin/ssh", Cmdline: []string{"ssh", "git@example.test"}}, owner}
	request.DefaultScope = 1
	request.ScopeAutomatic = true
	pending, results := submitPending(t, coordinator, request)
	client, err := DialController(startControlServer(t, coordinator))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = client.Close() }()
	hello := client.Pending()
	if len(hello) != 1 || !reflect.DeepEqual(hello[0].Scopes, request.Scopes) || hello[0].DefaultScope != 1 || !hello[0].ScopeAutomatic {
		t.Fatalf("hello lost command scopes: %+v", hello)
	}
	if err := client.Decide(pending.ID, true, time.Minute, 1); err != nil {
		t.Fatal(err)
	}
	nextControllerEvent(t, client, "ok")
	if result := <-results; result.Source != SourceController {
		t.Fatalf("first result = %+v, want controller", result)
	}
	sibling := request
	sibling.ClientPID++
	sibling.Scopes = []process.Summary{{PID: sibling.ClientPID, StartTime: 124, Exe: "/usr/bin/ssh-keygen"}, owner}
	result, err := coordinator.Submit(t.Context(), sibling)
	if result.Source != SourceMemory || err != nil {
		t.Fatalf("sibling result = %+v, %v; want memory", result, err)
	}
}

func TestControlRejectsInvalidScopeWithoutResolvingRequest(t *testing.T) {
	coordinator := newTestCoordinator(time.Second, notify.Noop())
	client := dialControl(t, startControlServer(t, coordinator))
	client.read(t)
	pending, results := submitPending(t, coordinator, testRequest("SHA256:invalid-scope"))
	client.readUntil(t, "pending")
	for _, command := range []controllerCommand{
		{Type: "decide", ID: pending.ID, Decision: "allow", RememberSeconds: 60, ScopeIndex: -1},
		{Type: "decide", ID: pending.ID, Decision: "allow", RememberSeconds: 60, ScopeIndex: 1},
		{Type: "decide", ID: pending.ID, Decision: "allow", RememberSeconds: -1},
		{Type: "decide", ID: pending.ID, Decision: "allow", RememberSeconds: int((1<<63-1)/time.Second) + 1},
	} {
		client.send(t, command)
		message := client.readUntil(t, "error")
		if message.ID != pending.ID || message.Error == "" {
			t.Fatalf("invalid error response: %+v", message)
		}
	}
	client.send(t, controllerCommand{Type: "decide", ID: pending.ID, Decision: "deny"})
	client.readUntil(t, "ok")
	if result := <-results; result.Outcome != OutcomeDenied {
		t.Fatalf("result = %+v, want denied", result)
	}
}
