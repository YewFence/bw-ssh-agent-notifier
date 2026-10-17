package proxy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/gate"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func TestGateSignatureHelper(t *testing.T) {
	if os.Getenv("BWSSHNTFR_TEST_SIGN") != "1" {
		return
	}
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(os.Getenv("BWSSHNTFR_TEST_PUBLIC_KEY")))
	if err != nil {
		t.Fatal(err)
	}
	conn, err := net.Dial("unix", os.Getenv("BWSSHNTFR_TEST_SOCKET"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	payload := []byte("scope integration test")
	signature, err := agent.NewClient(conn).Sign(key, payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := key.Verify(payload, signature); err != nil {
		t.Fatalf("upstream signature is invalid: %v", err)
	}
}

func TestServerRemembersSelectedParentAcrossRealChildCommands(t *testing.T) {
	dir := t.TempDir()
	upstreamPath := filepath.Join(dir, "upstream.sock")
	listenPath := filepath.Join(dir, "wrapper.sock")
	controlPath := filepath.Join(dir, "control.sock")
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring().(agent.ExtendedAgent)
	if err := keyring.Add(agent.AddedKey{PrivateKey: privateKey, Comment: "test-scope-key"}); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(privateKey)
	if err != nil {
		t.Fatal(err)
	}
	upstream, err := net.ListenUnix("unix", &net.UnixAddr{Name: upstreamPath, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upstream.Close() })
	go func() {
		for {
			conn, err := upstream.AcceptUnix()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_ = agent.ServeAgent(keyring, conn)
			}()
		}
	}()
	ctx, cancel := context.WithCancel(t.Context())
	server := Server{
		Config: config.Config{
			ListenSocket: listenPath, UpstreamSocket: upstreamPath, ControlSocket: controlPath,
			GateMode: config.GateModeConfirm, GateTimeout: 3 * time.Second, GateRememberTTL: time.Minute,
			ParentDepth: 2, NotifyBackend: "off",
			NotifyCallTimeout: time.Second, NotifyExpireTimeout: time.Second,
			LogLevel: config.DefaultLogLevel, LogFormat: config.DefaultLogFormat,
		},
		Notifier: notify.Noop(), Logger: slog.New(slog.NewTextHandler(io.Discard, nil)),
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-serverDone; err != nil {
			t.Errorf("server.Run(): %v", err)
		}
	})
	waitForSocket(t, listenPath)
	waitForSocket(t, controlPath)
	controller, err := gate.DialController(controlPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = controller.Close() })
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("BWSSHNTFR_TEST_SIGN", "1")
	t.Setenv("BWSSHNTFR_TEST_SOCKET", listenPath)
	t.Setenv("BWSSHNTFR_TEST_PUBLIC_KEY", string(ssh.MarshalAuthorizedKey(signer.PublicKey())))
	first := exec.CommandContext(ctx, executable, "-test.run=^TestGateSignatureHelper$")
	firstDone := make(chan error, 1)
	go func() {
		output, err := first.CombinedOutput()
		if err != nil {
			t.Errorf("first helper: %v\n%s", err, output)
		}
		firstDone <- err
	}()
	var request gate.Request
	select {
	case event := <-controller.Events():
		if event.Type != "pending" || event.Request == nil {
			t.Fatalf("expected pending request, got %+v", event)
		}
		request = *event.Request
	case <-time.After(5 * time.Second):
		t.Fatal("helper did not submit a signature request")
	}
	if request.DefaultScope != 0 || request.ScopeAutomatic {
		t.Fatal("unconfigured request should default to requester")
	}
	ownerIndex := -1
	for index, scope := range request.Scopes {
		if scope.PID == os.Getpid() {
			ownerIndex = index
		}
	}
	if ownerIndex <= 0 {
		t.Fatalf("parent test process not selectable: %+v", request.Scopes)
	}
	if err := controller.Decide(request.ID, true, time.Minute, ownerIndex); err != nil {
		t.Fatal(err)
	}
	if err := <-firstDone; err != nil {
		t.Fatal(err)
	}
	second := exec.CommandContext(ctx, executable, "-test.run=^TestGateSignatureHelper$", "-test.v")
	if output, err := second.CombinedOutput(); err != nil {
		t.Fatalf("new child with different argv did not reuse parent grant: %v\n%s", err, output)
	}
	for {
		select {
		case event := <-controller.Events():
			if event.Type == "pending" {
				t.Fatal("second child prompted instead of reusing the selected parent's grant")
			}
		default:
			return
		}
	}
}
