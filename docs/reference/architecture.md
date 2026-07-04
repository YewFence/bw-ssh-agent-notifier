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

## Forwarding model

bw-ssh-agent-notifier does not parse SSH agent messages. Each client connection gets a matching upstream connection to Bitwarden, then bytes are copied in both directions until either side closes or errors.

This means bw-ssh-agent-notifier does not need to understand OpenSSH key formats, agent request types, or signing payloads.

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

bw-ssh-agent-notifier does not:

- approve or reject SSH agent requests
- replace Bitwarden authorization
- pass the real application name into Bitwarden
- parse SSH agent protocol messages
- implement key fingerprint rules
- implement allow lists or deny lists
- modify Bitwarden, Flatpak, OpenSSH, or system SSH configuration

The real authorization decision remains in Bitwarden. bw-ssh-agent-notifier only adds local notifications and logs around the request.
