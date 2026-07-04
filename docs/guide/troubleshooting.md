# Troubleshooting

Start with `doctor`. It checks the pieces that bw-ssh-agent-notifier needs before it can forward SSH agent traffic and send notifications.

```bash
bwsshntfr doctor
```

For a user service, read the logs from the user journal.

```bash
journalctl --user -u bw-ssh-agent-notifier.service -f
```

## Bitwarden upstream socket is missing

bw-ssh-agent-notifier forwards requests to the Bitwarden SSH agent socket. The Flatpak default is:

```text
$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock
```

If `doctor` reports that the upstream socket is missing, check that Bitwarden Desktop is running and that SSH agent support is enabled in Bitwarden. Then verify the socket path:

```bash
ls -l "$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock"
```

If your Bitwarden socket lives somewhere else, set `BITWARDEN_SSH_AGENT_SOCKET` or pass `--upstream`.

```bash
BITWARDEN_SSH_AGENT_SOCKET=/path/to/bitwarden-agent.sock bwsshntfr doctor
bwsshntfr --upstream /path/to/bitwarden-agent.sock run
```

For a generated systemd user service, regenerate the service with the upstream flag so the environment line is written into the service file.

```bash
bwsshntfr --upstream /path/to/bitwarden-agent.sock systemd print-user-service > "$HOME/.config/systemd/user/bw-ssh-agent-notifier.service"
systemctl --user daemon-reload
systemctl --user restart bw-ssh-agent-notifier.service
```

## Wrapper socket is not used by SSH

bw-ssh-agent-notifier only sees requests from clients that use its wrapper socket. Check the current shell environment:

```bash
printf '%s\n' "$SSH_AUTH_SOCK"
```

It should point to:

```text
$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock
```

For a quick manual test:

```bash
export SSH_AUTH_SOCK="$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock"
ssh-add -l
```

If SSH works but no bw-ssh-agent-notifier log entry appears, the process probably still uses another agent socket.

## Session D-Bus or notifications fail

bw-ssh-agent-notifier sends desktop notifications through the freedesktop notifications D-Bus interface. A notification failure does not stop SSH agent forwarding.

Test only the notification path:

```bash
bwsshntfr notify test
```

If this fails, check whether the process has access to the user session bus:

```bash
printf '%s\n' "$DBUS_SESSION_BUS_ADDRESS"
```

When running under `systemd --user`, inspect the service logs for the exact D-Bus error:

```bash
journalctl --user -u bw-ssh-agent-notifier.service -n 100
```

You can disable notifications and keep forwarding enabled:

```bash
bwsshntfr --notify off run
```

## Service starts but exits quickly

Check the service status first:

```bash
systemctl --user status bw-ssh-agent-notifier.service
```

Common causes are a missing upstream Bitwarden socket, an unwritable wrapper socket directory, or a service file that points to a binary path that no longer exists.

If the binary was installed through mise, consider changing the generated service file to resolve the binary at service start time instead of embedding the current absolute binary path:

```ini
ExecStart=mise x -- bwsshntfr
```

Then reload and restart the service:

```bash
systemctl --user daemon-reload
systemctl --user restart bw-ssh-agent-notifier.service
```

## Wrapper socket already exists

If the wrapper socket path already exists, bw-ssh-agent-notifier checks whether another instance is listening there. If another instance is active, the new process exits instead of taking over the socket.

Check whether a service is already running:

```bash
systemctl --user status bw-ssh-agent-notifier.service
```

If there is no running service and the socket is stale, starting bw-ssh-agent-notifier again should remove the stale socket and recreate it.

## Process information is incomplete

bw-ssh-agent-notifier reads process metadata from `/proc`. If the client process exits quickly, or if `/proc` access is restricted, logs and notifications may contain only partial information.

Use `debug inspect-pid` to test process inspection directly:

```bash
bwsshntfr debug inspect-pid "$$"
```

Increase parent process depth if the useful process name is farther up the process tree:

```bash
bwsshntfr --parent-depth 8 run
```
