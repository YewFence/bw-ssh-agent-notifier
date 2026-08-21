# bw-ssh-agent-notifier

[![Release](https://img.shields.io/github/v/release/YewFence/bw-ssh-agent-notifier?sort=semver)](https://github.com/YewFence/bw-ssh-agent-notifier/releases)
[![Docs](https://img.shields.io/badge/docs-online-blue)](https://YewFence.github.io/bw-ssh-agent-notifier/)
[![License](https://img.shields.io/github/license/YewFence/bw-ssh-agent-notifier)](LICENSE)

bw-ssh-agent-notifier is a small Linux desktop helper that shows which local process is using the Bitwarden SSH agent. It creates a local Unix socket for `SSH_AUTH_SOCK`, records which process connects to it, sends a desktop notification, and transparently forwards SSH agent traffic to the Bitwarden agent socket. An optional gate mode additionally holds every signature request until you allow or deny it from a terminal UI.

> [!NOTE]
> This project is in an early development stage. Core features may be missing, and backward compatibility is not guaranteed.

## What it does

Bitwarden Desktop Flatpak can act as an SSH agent, but the Flatpak sandbox cannot identify the host-side process that triggered an authorization request. As a result, Bitwarden always shows `Unknown application` in the authorization popup.

bw-ssh-agent-notifier does not replace Bitwarden authorization. It runs in front of the Bitwarden SSH agent and adds local observability:

- Listens on a wrapper socket, by default `$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock`.
- Reads Linux peer credentials from incoming Unix socket connections.
- Inspects `/proc/<pid>/exe`, `/proc/<pid>/cmdline`, and a limited parent process chain.
- Sends a desktop notification such as `ssh is using Bitwarden SSH agent`.
- Logs process and forwarding details to standard output, which works well with `systemd --user` and `journalctl --user`.
- Transparently forwards SSH agent protocol bytes to the Bitwarden agent socket.

## Boundaries

By default bw-ssh-agent-notifier is intentionally small: it does not parse the SSH agent protocol, approve or reject requests, or change Bitwarden, Flatpak, OpenSSH, or desktop environment configuration. That changes only when you opt into the gate mode, which intercepts signature requests and asks for a decision.

The gate is a guard against your own mistakes, such as pushing to the wrong branch, not a security barrier: any process running as the same user can connect to the sockets, so malware under your account is not stopped by it. The gate also cannot pass the real application name into Bitwarden, because the SSH agent protocol does not include that field.

## Quick Start

### Installation

#### Mise

```bash
mise use --global github:YewFence/bw-ssh-agent-notifier
```

#### Go

```bash
go install github.com/YewFence/bw-ssh-agent-notifier/cmd/bwsshntfr@latest
```

#### Build and install from source locally

```bash
git clone https://github.com/YewFence/bw-ssh-agent-notifier.git
cd bw-ssh-agent-notifier
mise trust
mise install
mise run cli:install
```

### Start the daemon manually

```bash
bwsshntfr run
```

Then open a new shell and point SSH clients at the wrapper socket.

```bash
export SSH_AUTH_SOCK="$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock"
ssh-add -l
```

If you see your Bitwarden SSH keys listed and a desktop notification appears, the wrapper is working as expected.

### Install as user service (Recommended)

```bash
mkdir -p "$HOME/.config/systemd/user"
bwsshntfr systemd print-user-service > "$HOME/.config/systemd/user/bw-ssh-agent-notifier.service"
systemctl --user daemon-reload
systemctl --user enable --now bw-ssh-agent-notifier.service
```

Then point SSH clients at the wrapper socket.

```bash
export SSH_AUTH_SOCK="$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock"
```

For advanced users, you can copy the service template in [`internal/systemd/bitwarden-ssh-agent-wrapper.service`](internal/systemd/bitwarden-ssh-agent-wrapper.service) and edit it manually.

### Observe SSH agent usage

When a local process connects to the wrapper socket, you will see a desktop notification like this:

```text
SSH agent request
ssh-add is using Bitwarden SSH agent
PID 44746 · /usr/bin/ssh-add
Process tree ssh-add <- zsh
```

You can also audit the usage in journalctl:

```bash
journalctl --user -u bw-ssh-agent-notifier.service
```

Output example:

```text
time=<timestamp> level=INFO msg="ssh agent client connected" client_pid=271175 client_uid=1000 client_gid=1000 upstream_socket=/home/yewfence/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock client_exe=/usr/bin/ssh-add client_cmdline="ssh-add -l" parent_chain="zsh <- ghostty <- systemd <- systemd"
```

## Optional signature gate

Bitwarden's own approval can be told to remember a decision and then it stops asking, which is exactly when mistakes slip through. The gate mode takes the per-use confirmation back and adds what Bitwarden cannot show: the calling process.

Enable it on the daemon (the wrapper socket stays the same):

```bash
bwsshntfr run --gate confirm
```

Then open the terminal UI that approves or denies signature requests:

```bash
bwsshntfr gate
```

Every `SSH_AGENTC_SIGN_REQUEST` now waits for a decision. The UI shows the key name or fingerprint, the client command line, and the parent process chain, so `jj git push -b wrongbranch` is distinguishable from the branch you meant. Other requests, such as key listing, pass through untouched; key management operations are always rejected.

The gate fails closed. If no `bwsshntfr gate` controller is connected, or a request waits longer than `--gate-timeout` (default 60s), the signature is denied and a desktop notification explains why.

Pressing `m` instead of `a` allows the request and remembers the decision for a chosen TTL (default 1m, presets 1m/5m/15m, custom input). A remembered decision replays silently for the same key and the same normalized process chain — shells, multiplexers, and terminal emulators are stripped, so launching inside zellij does not invalidate the memory, but a different branch in the push command does. Memory is in-RAM only: restarting the daemon clears it, and `--gate-remember-ttl=0` disables it entirely. Deciding one request also clears every other pending request with the same key and chain, so bursts do not force you to press `y` repeatedly.

## Documentation

See the [documentation site](https://YewFence.github.io/bw-ssh-agent-notifier) for more information.

## Contributing

If you have suggestions or find a bug, please [open an issue](https://github.com/YewFence/bw-ssh-agent-notifier/issues).

Pull requests are welcome. See the [Contributing Guide](CONTRIBUTING.md).

## License

[MIT License](LICENSE)
