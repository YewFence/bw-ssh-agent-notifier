# Configuration

bw-ssh-agent-notifier reads configuration from command line flags, environment variables, and built-in defaults.

The precedence is:

```text
command line flag > environment variable > default value
```

## Runtime Paths

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Wrapper socket | `--listen` | `WRAPPER_SSH_AGENT_SOCKET` | `$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock` |
| Bitwarden upstream socket | `--upstream` | `BITWARDEN_SSH_AGENT_SOCKET` | `$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock` |

The wrapper socket is the socket that SSH clients should use through `SSH_AUTH_SOCK`. The upstream socket is the real Bitwarden SSH agent socket that bw-ssh-agent-notifier forwards traffic to.

## Notifications

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Notification backend | `--notify` | `WRAPPER_NOTIFY_BACKEND` | `dbus` |
| Notification call timeout | `--notify-call-timeout` | `WRAPPER_NOTIFY_CALL_TIMEOUT` | `2s` |
| Notification expire timeout | `--notify-expire-timeout` | `WRAPPER_NOTIFY_EXPIRE_TIMEOUT` | `4s` |

Supported notification backends:

| Value | Behavior |
| --- | --- |
| `dbus` | Send desktop notifications through `org.freedesktop.Notifications` on the session bus. |
| `off` | Disable notifications while still forwarding SSH agent traffic and writing logs. |

Notification call timeout determines how long to wait for a D-Bus response. Notification expire timeout determines how long the notification is displayed.

Notification failures are logged and do not block forwarding.

## Process Inspection

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Parent process depth | `--parent-depth` | `WRAPPER_PARENT_DEPTH` | `5` |

bw-ssh-agent-notifier always records peer credentials from the Unix socket when available. It then inspects `/proc/<pid>/exe`, `/proc/<pid>/cmdline`, and `/proc/<pid>/status`. The parent depth controls how far it follows `PPid` entries from `/proc/<pid>/status`.

## Logging

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Log level | `--log-level` | No environment variable | `info` |
| Log format | `--log-format` | No environment variable | `text` |

Supported log levels are `debug`, `info`, `warn`, and `error`. Supported log formats are `text` and `json`.

When running as a systemd user service, logs are written to standard output and can be read with:

```bash
journalctl --user -u bw-ssh-agent-notifier.service -f
```
