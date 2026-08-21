# Architecture

bw-ssh-agent-notifier is a local proxy in front of the Bitwarden SSH agent. It adds visibility around who is using the agent, while leaving SSH agent authorization to Bitwarden.

```text
ssh / git / ssh-add
        |
        | SSH_AUTH_SOCK
        v
bwsshntfr wrapper socket
        |
        | peer credentials + /proc inspection + notification + log
        v
Bitwarden Flatpak SSH agent socket
        |
        v
Bitwarden authorization popup
```

## Why it exists

Bitwarden Desktop can act as an SSH agent, but when Bitwarden Desktop runs in the Flatpak sandbox, it cannot identify the host-side application that triggered an SSH authorization request. Bitwarden may show `Unknown application` in the authorization popup.

bw-ssh-agent-notifier listens on a socket that your local SSH clients use. When a client connects, bw-ssh-agent-notifier reads Linux peer credentials, inspects `/proc`, sends a desktop notification, writes a log entry, and forwards all bytes to the Bitwarden agent socket.

## Forwarding model (default mode)

bw-ssh-agent-notifier does not parse SSH agent messages. Each client connection gets a matching upstream connection to Bitwarden, then bytes are copied in both directions until either side closes or errors.

This means bw-ssh-agent-notifier does not need to understand OpenSSH key formats, agent request types, or signing payloads.

## Gate mode

With `--gate confirm` the daemon keeps one upstream connection per client but replaces the byte pipe on the client side with a protocol-aware agent. Both directions are handled by `golang.org/x/crypto/ssh/agent`: the client side is served through `agent.ServeAgent` and the upstream side is wrapped with `agent.NewClient`, so no wire format is parsed by this project.

```text
ssh / git / jj
   | SSH_AUTH_SOCK (same wrapper socket)
   v
agent.ServeAgent(gate.Agent)      List is forwarded; SignWithFlags waits for a decision
   |                              Add/Remove/Lock/Extension are always rejected
   v
agent.NewClient(upstream)
   |
   v
Bitwarden socket
```

Every `SSH_AGENTC_SIGN_REQUEST` becomes a pending decision that carries the key fingerprint, the key comment resolved from the upstream key list, and the client process information from peer credentials and `/proc`. The daemon exposes a control socket speaking JSON-lines; `bwsshntfr gate` is the built-in controller, and any client implementing the protocol can act as one.

The gate fails closed: no connected controller or a decision timeout denies the signature, and a desktop notification explains why.

### Remembered decisions

Allowing with `m` stores a remembered decision keyed by `key fingerprint + client command line + normalized parent chain`. The normalization strips shells, multiplexers such as tmux and zellij, terminal emulators, and session infrastructure, because those describe where a command runs, not what it runs. Branch names inside a `git push` or `jj git push` command line survive normalization, so pushing to a different branch always asks again. The TTL is a fixed window (default 1m, configurable, 0 disables), memory is in-RAM only, and a decision applies to every pending request sharing the same key.

## Process identification

For Unix socket clients, bw-ssh-agent-notifier reads peer credentials from the kernel. It uses the process ID to inspect:

```text
/proc/<pid>/exe
/proc/<pid>/cmdline
/proc/<pid>/status
```

`/proc/<pid>/status` provides the parent process ID. bw-ssh-agent-notifier follows that parent chain up to the configured depth so logs can show context such as `ssh <- zsh <- foot`.

Process inspection is best-effort. If the process exits quickly or `/proc` access is limited, forwarding still continues.

## Notifications

The default notification backend calls the freedesktop desktop notifications interface over the session D-Bus:

```text
service: org.freedesktop.Notifications
object: /org/freedesktop/Notifications
method: org.freedesktop.Notifications.Notify
```

Notifications use a short timeout. If the session bus or notification service is unavailable, bw-ssh-agent-notifier logs the failure and continues forwarding.

## Boundaries

In default mode bw-ssh-agent-notifier does not:

- approve or reject SSH agent requests
- replace Bitwarden authorization
- pass the real application name into Bitwarden
- parse SSH agent protocol messages
- implement key fingerprint rules
- implement allow lists or deny lists
- modify Bitwarden, Flatpak, OpenSSH, or system SSH configuration

Gate mode deliberately crosses the first two lines for signature requests only. It guards against your own mistakes, such as pushing to the wrong branch; it is not a security barrier, because any process running as the same user can connect to the sockets directly.

Vault unlocking and key custody always remain in Bitwarden.
