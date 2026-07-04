# Commands

The root command starts the wrapper. `run` is an explicit alias for the same runtime behavior.

```bash
bwsshntfr
bwsshntfr run
```

Global configuration flags can be used on the root command and subcommands that need runtime configuration.

```text
--listen
--upstream
--notify
--notify-call-timeout
--parent-depth
--log-level
--log-format
```

## doctor

`doctor` checks the local runtime environment without starting the proxy.

```bash
bwsshntfr doctor
```

It checks:

- `XDG_RUNTIME_DIR`
- wrapper socket parent directory
- Bitwarden upstream socket
- session D-Bus notification path
- `/proc` process inspection

## notify test

`notify test` sends a test desktop notification. It does not access the SSH agent socket.

```bash
bwsshntfr notify test
bwsshntfr notify test --summary "SSH agent request" --body "test notification"
```

Use this when forwarding works but notifications do not appear.

## debug inspect-pid

`debug inspect-pid` prints the process information that bwsshntfr can read for a specific process.

```bash
bwsshntfr debug inspect-pid 12345
bwsshntfr debug inspect-pid "$$"
```

It prints the process ID, user ID, group ID, executable path, command line, and parent process chain.

## systemd print-user-service

`systemd print-user-service` prints a systemd user service to standard output. It does not write files and does not call `systemctl`.

```bash
bwsshntfr systemd print-user-service
```

Recommended installation:

```bash
mkdir -p "$HOME/.config/systemd/user"
bwsshntfr systemd print-user-service > "$HOME/.config/systemd/user/bw-ssh-agent-notifier.service"
systemctl --user daemon-reload
systemctl --user enable --now bw-ssh-agent-notifier.service
```

## completion

`completion` prints shell completion scripts.

```bash
bwsshntfr completion bash
bwsshntfr completion zsh
bwsshntfr completion fish
bwsshntfr completion powershell
```

See [Shell Completion](/guide/completion) for installation examples.

## version

`version` prints the current bwsshntfr version.

```bash
bwsshntfr version
```
