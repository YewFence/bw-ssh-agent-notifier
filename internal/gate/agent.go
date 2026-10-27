package gate

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

var ErrRejected = errors.New("operation rejected by gate")

// Agent is the SSH agent the gate serves to local clients. Signature requests
// go through the Coordinator for a decision; every other mutating operation is
// rejected so nothing can manage keys through the gate.
type Agent struct {
	ConnCtx       context.Context
	ClientConn    *net.UnixConn
	Upstream      agent.ExtendedAgent
	Coordinator   *Coordinator
	Client        process.Info
	ParentDepth   int
	CommandGroups [][]string
	Logger        *slog.Logger
}

func (gateAgent *Agent) List() ([]*agent.Key, error) {
	return gateAgent.Upstream.List()
}

func (gateAgent *Agent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	return gateAgent.SignWithFlags(key, data, 0)
}

func (gateAgent *Agent) SignWithFlags(key ssh.PublicKey, data []byte, flags agent.SignatureFlags) (*ssh.Signature, error) {
	ctx := gateAgent.ConnCtx
	if ctx == nil {
		ctx = context.Background()
	}
	client, _ := process.Inspect(gateAgent.Client.PID, gateAgent.ParentDepth)
	if client.StartTime != gateAgent.Client.StartTime {
		return nil, ErrRejected
	}
	gateAgent.Client = client
	ctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	if gateAgent.ClientConn != nil {
		go watchClientDisconnect(ctx, gateAgent.ClientConn, cancel, gateAgent.Logger)
	}
	if _, err := gateAgent.Coordinator.Submit(ctx, gateAgent.buildRequest(key)); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return gateAgent.Upstream.SignWithFlags(key, data, flags)
}

func (gateAgent *Agent) buildRequest(key ssh.PublicKey) Request {
	clientSummary := process.Summary{PID: gateAgent.Client.PID, Exe: gateAgent.Client.Exe, Cmdline: gateAgent.Client.Cmdline}
	scopes := process.CommandScopes(gateAgent.Client)
	defaultScope, automatic := process.DefaultScope(scopes, gateAgent.CommandGroups)
	return Request{
		KeyFormat:      key.Type(),
		KeyFingerprint: ssh.FingerprintSHA256(key),
		KeyComment:     gateAgent.lookupComment(key),
		ClientPID:      gateAgent.Client.PID,
		ClientExe:      gateAgent.Client.Exe,
		ClientCmdline:  process.CommandLine(gateAgent.Client.Cmdline),
		ParentChain:    process.ProcessChain(clientSummary, gateAgent.Client.Parents),
		Scopes:         scopes,
		DefaultScope:   defaultScope,
		ScopeAutomatic: automatic,
	}
}

func (gateAgent *Agent) lookupComment(key ssh.PublicKey) string {
	logger := gateAgent.Logger
	if logger == nil {
		logger = slog.Default()
	}
	keys, err := gateAgent.Upstream.List()
	if err != nil {
		logger.Debug("list upstream keys for comment lookup failed", "error", err)
		return ""
	}
	blob := key.Marshal()
	for _, listed := range keys {
		if bytes.Equal(listed.Marshal(), blob) {
			return listed.Comment
		}
	}
	return ""
}

func (gateAgent *Agent) Add(agent.AddedKey) error {
	return ErrRejected
}

func (gateAgent *Agent) Remove(ssh.PublicKey) error {
	return ErrRejected
}

func (gateAgent *Agent) RemoveAll() error {
	return ErrRejected
}

func (gateAgent *Agent) Lock([]byte) error {
	return ErrRejected
}

func (gateAgent *Agent) Unlock([]byte) error {
	return ErrRejected
}

func (gateAgent *Agent) Signers() ([]ssh.Signer, error) {
	return nil, ErrRejected
}

func (gateAgent *Agent) Extension(string, []byte) ([]byte, error) {
	return nil, agent.ErrExtensionUnsupported
}
