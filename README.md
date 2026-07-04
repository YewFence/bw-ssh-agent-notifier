# bw-ssh-agent-notifier

[![Release](https://img.shields.io/github/v/release/YewFence/bw-ssh-agent-notifier?sort=semver)](https://github.com/YewFence/bw-ssh-agent-notifier/releases)
[![Docs](https://img.shields.io/badge/docs-online-blue)](https://YewFence.github.io/bw-ssh-agent-notifier/)
[![License](https://img.shields.io/github/license/YewFence/bw-ssh-agent-notifier)](LICENSE)

bw-ssh-agent-notifier is a small Linux desktop helper that shows which local process is using the Bitwarden SSH agent. It creates a local Unix socket for `SSH_AUTH_SOCK`, records which process connects to it, sends a desktop notification, and transparently forwards SSH agent traffic to the Bitwarden agent socket.

> [!NOTE]
> This project is in an early development stage. Core features may be missing, and backward compatibility is not guaranteed.

## What it does

Bitwarden Desktop Flatpak can act as an SSH agent, but the Flatpak sandbox cannot always identify the host-side process that triggered an authorization request. As a result, Bitwarden may show `Unknown application` in the authorization popup.

bw-ssh-agent-notifier does not replace Bitwarden authorization. It runs in front of the Bitwarden SSH agent and adds local observability:

- Listens on a wrapper socket, by default `$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock`.
- Reads Linux peer credentials from incoming Unix socket connections.
- Inspects `/proc/<pid>/exe`, `/proc/<pid>/cmdline`, and a limited parent process chain.
- Sends a desktop notification such as `ssh is using Bitwarden SSH agent`.
- Logs process and forwarding details to standard output, which works well with `systemd --user` and `journalctl --user`.
- Transparently forwards SSH agent protocol bytes to the Bitwarden agent socket.

## Boundaries

bw-ssh-agent-notifier is intentionally small. It does not parse the SSH agent protocol, approve or reject SSH agent requests, implement allow lists or deny lists, or change Bitwarden, Flatpak, OpenSSH, or desktop environment configuration.

Authorization still happens in Bitwarden. bw-ssh-agent-notifier only helps show which local process is using the agent before the request reaches Bitwarden. It also cannot pass the real application name into Bitwarden, because the SSH agent protocol does not include that field.

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

The systemd user service template lives in [`internal/systemd/bitwarden-ssh-agent-wrapper.service`](internal/systemd/bitwarden-ssh-agent-wrapper.service).

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

If `bwsshntfr` is installed with mise, the service can avoid embedding the current absolute binary path by changing `ExecStart` to use mise.

```ini
ExecStart=mise x -- bwsshntfr
```

## Documentation

See the [documentation site](https://YewFence.github.io/bw-ssh-agent-notifier) for more information.

## Contributing

If you have suggestions or find a bug, please [open an issue](https://github.com/YewFence/bw-ssh-agent-notifier/issues).

Pull requests are welcome. See the [Contributing Guide](CONTRIBUTING.md).

## License

[MIT License](LICENSE)
