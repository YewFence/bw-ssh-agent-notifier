# Signature Gate

The gate is an opt-in mode that turns bw-ssh-agent-notifier from an observer into a permission prompt for signature requests. It exists for the moment Bitwarden's own approval popup no longer asks: Bitwarden can be told to remember a decision, and from then on a typo such as `jj git push -b main` slips straight through. The gate takes the per-use confirmation back and shows what Bitwarden cannot: the exact command line that wants to sign.

> [!WARNING]
> The gate guards against your own mistakes. It is not a security barrier: any process running as the same user can connect to the wrapper or control sockets directly.

## Enable the gate

Start the daemon in confirm mode. The wrapper socket stays the same, so `SSH_AUTH_SOCK` needs no change.

```bash
bwsshntfr run --gate confirm
```

For a systemd user service, add the flag to `ExecStart` or set `WRAPPER_GATE=confirm` in the unit.

## Open the controller

```bash
bwsshntfr gate
```

Every `SSH_AGENTC_SIGN_REQUEST` now waits for a decision. The TUI shows one card per pending request:

```text
▶ #1  key: smoke-key  timeout: 42s
  requester:      ssh git@github.com
  pid:            311351
  executable:     /usr/bin/ssh
  tree:           ssh <- git <- jj <- zsh <- foot
  fingerprint:    SHA256:4l1H5hoUp0QLoZWSJ1HYARwU7GgMTHWzWwgGg9FxOew
  commands:       jj > git > [ssh]
  remember scope: ssh git@github.com
  scope pid:      311351
  scope exe:      /usr/bin/ssh
  selection:      default: requester
```

Keys:

| Key | Action |
| --- | --- |
| `a` / `y` | Allow this request once |
| `m` | Allow and remember: pick 1m / 5m / 15m / custom, the global default is preselected |
| `d` / `n` | Deny this request |
| `j` / `k`, up / down | Select a pending request |
| `h` / `l`, left / right | Select the command scope: left toward ancestors, right toward the requester |
| `q`, `Ctrl+C` | Quit (pending requests keep waiting until timeout) |

Changing command scope takes effect immediately in the UI; there is no extra Enter step and navigation never approves anything. The selected command's complete argv, PID, executable, and whether it was selected automatically or manually are shown separately from the actual requester. In the duration picker, the existing navigation and Enter confirmation still apply.

One-shot allow and deny decisions also resolve pending requests from the same requester instance using the same key. Remembering an allow additionally resolves pending requests covered by the selected command scope; unrelated commands and other keys keep waiting.

## Fail-closed behavior

- If no controller is connected, the signature is denied immediately.
- If no decision arrives within `--gate-timeout` (default 60s), the signature is denied.
- Every denial sends a desktop notification explaining the reason, and both events are logged to the journal.

Key listing and other read requests pass through untouched; key management requests (`ssh-add -D`, lock, extensions) are always rejected by the gate.

## Remembered decisions

`m` grants the selected command instance and its descendants permission to use this key for a fixed, non-renewing window. Instances are identified by PID and kernel process start time, not their command text: a new invocation of the same command must ask again. The grant expires when its TTL ends or the selected process exits, whichever happens first.

The default scope is the direct requester, often `ssh` or `ssh-keygen`. Move left to select an ancestor such as the current `bd sync`, `go` invocation, or `jj git push`. Its internal helpers may then change their command arguments, reconnect, or run concurrently without asking again for the same key. Shells, multiplexers, terminal emulators, and session infrastructure are displayed as context but are not selectable scopes. Available ancestors are limited by `--parent-depth` (default 5).

This does not grant the key globally or authorize a future invocation of the selected command. Choosing a broad, long-running ancestor deliberately grants that instance's other descendants access to the same key until expiration. If process identity cannot be inspected, one-shot decisions remain available but remembering is disabled.

Remembered requests pass silently (a journal entry records the replay). Memory lives in RAM only: restarting the daemon forgets everything. Set `--gate-remember-ttl=0` to disable remembering, or a custom default such as `5m` for longer quiet windows.

## Automatically select command groups

Presets select a suitable command scope; they never approve a request. Each group is a JSON argv prefix containing an executable name or absolute path followed by optional exact argument prefixes. Repeat the flag to define several groups:

```bash
bwsshntfr run --gate confirm \
  --gate-command-group '["bd","sync"]' \
  --gate-command-group '["jj","git","push"]'
```

The environment variable takes a JSON array of groups:

```bash
WRAPPER_GATE=confirm \
WRAPPER_GATE_COMMAND_GROUPS='[["bd","sync"],["jj","git","push"]]' \
bwsshntfr run
```

The nearest matching process wins, regardless of group order. With no match, selection stays at the requester. Matching uses complete argv elements, not substrings or regular expressions. An absolute executable path must match the inspected executable path. Command-line groups replace environment groups. Manual scope changes override the preset for that request and survive request switching and controller state refreshes.

## Troubleshooting

- `bwsshntfr gate` fails to connect: the daemon is not running or not in confirm mode. Check `WRAPPER_GATE`/`--gate`.
- Every signature is denied with `no-controller`: the controller connection dropped; reopen `bwsshntfr gate`.
- A remembered allow stopped working: the daemon restarted, the TTL expired, the selected command exited, or the request comes from outside that command's inspected descendant chain. Running the same command again is a new process instance.
- A useful ancestor is missing: increase `--parent-depth`; shells and session infrastructure cannot be selected.
- To audit scope decisions, look for `command_scopes` on connection logs and `scope_pid`, `scope_start_time`, and `scope_cmdline` on remembered-decision logs.
