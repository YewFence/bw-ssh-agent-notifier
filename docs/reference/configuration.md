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

## Gate

The gate is an opt-in mode that intercepts every signature request and waits for an allow or deny decision from a controller such as `bwsshntfr gate`. See the [gate guide](/guide/gate) for the full behavior.

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Gate mode | `--gate` | `WRAPPER_GATE` | `off` |
| Decision timeout | `--gate-timeout` | `WRAPPER_GATE_TIMEOUT` | `60s` |
| Default remember TTL | `--gate-remember-ttl` | `WRAPPER_GATE_REMEMBER_TTL` | `1m` |
| Automatic command scopes | `--gate-command-group` (repeatable JSON argv prefix) | `WRAPPER_GATE_COMMAND_GROUPS` (JSON array of argv prefixes) | None |
| Control socket | `--control-socket` | `WRAPPER_CONTROL_SOCKET` | `$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper-control.sock` |

Supported gate modes:

| Value | Behavior |
| --- | --- |
| `off` | Transparent forwarding with notifications only. No SSH agent protocol parsing. |
| `confirm` | Intercept `SSH_AGENTC_SIGN_REQUEST`; every other request type is forwarded, and key management requests are always rejected. |

The gate fails closed: with no controller connected, or when no decision arrives within the decision timeout, the signature is denied and a desktop notification explains the reason.

The remember TTL is a fixed, non-renewing window applied when you allow a request with `m` in the TUI. A grant covers the selected process instance and its descendants for this key only, and ends at expiration or process exit. A new invocation of the same command does not inherit the grant. Set `0` to disable remembering entirely.

Command groups only choose the initial scope; they do not authorize requests. For example, `--gate-command-group '["bd","sync"]'` selects the nearest `bd sync` ancestor. The equivalent environment value is `WRAPPER_GATE_COMMAND_GROUPS='[["bd","sync"]]'`. Groups match executable names or absolute paths and exact argv prefixes. CLI groups replace environment groups. See [automatic command groups](/guide/gate#automatically-select-command-groups).

## Process Inspection

| Setting | Flag | Environment variable | Default |
| --- | --- | --- | --- |
| Parent process depth | `--parent-depth` | `WRAPPER_PARENT_DEPTH` | `5` |

bw-ssh-agent-notifier always records peer credentials from the Unix socket when available. It then inspects `/proc/<pid>/exe`, `/proc/<pid>/cmdline`, `/proc/<pid>/status`, and `/proc/<pid>/stat`. The parent depth controls how far it follows `PPid` entries from `/proc/<pid>/status`, including which ancestors are available for scope selection. Kernel start times distinguish process instances when PIDs are reused.

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
