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
▶ #1  smoke-key
      ssh git@github.com
      PID 311351 · /usr/bin/ssh
      tree: ssh <- git <- jj <- zsh <- foot
      42s  SHA256:4l1H5hoUp0QLoZWSJ1HYARwU7GgMTHWzWwgGg9FxOew
```

Keys:

| Key | Action |
| --- | --- |
| `a` / `y` | Allow this request once |
| `m` | Allow and remember: pick 1m / 5m / 15m / custom, the global default is preselected |
| `d` / `n` | Deny this request |
| `j` / `k`, arrows | Move the selection |
| `q`, `Ctrl+C` | Quit (pending requests keep waiting until timeout) |

A decision applies to every pending request with the same key and process chain, so a burst of identical requests clears with one keypress.

## Fail-closed behavior

- If no controller is connected, the signature is denied immediately.
- If no decision arrives within `--gate-timeout` (default 60s), the signature is denied.
- Every denial sends a desktop notification explaining the reason, and both events are logged to the journal.

Key listing and other read requests pass through untouched; key management requests (`ssh-add -D`, lock, extensions) are always rejected by the gate.

## Remembered decisions

`m` stores an allow decision for a fixed, non-renewing window. The memory key is the key fingerprint plus the client command line plus the parent chain with wrapper processes stripped:

- shells, multiplexers (`tmux`, `zellij`, `screen`), terminal emulators, and session infrastructure are removed, so starting the same push inside zellij does not invalidate the memory;
- branch names inside a push command line survive, so `jj git push -b wrongbranch` asks again even right after you allowed `-b main`.

Remembered requests pass silently (a journal entry records the replay). Memory lives in RAM only: restarting the daemon forgets everything. Set `--gate-remember-ttl=0` to disable remembering, or a custom default such as `5m` for longer quiet windows.

## Troubleshooting

- `bwsshntfr gate` fails to connect: the daemon is not running or not in confirm mode. Check `WRAPPER_GATE`/`--gate`.
- Every signature is denied with `no-controller`: the controller connection dropped; reopen `bwsshntfr gate`.
- A remembered allow stopped working: the daemon restarted (memory is RAM-only), the TTL expired, or the command line changed.
