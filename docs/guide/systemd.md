# Systemd User Service

bw-ssh-agent-notifier is recommended to run as a `systemd --user` service, so logs stay in the user journal and the wrapper socket is available to shells and desktop applications.

## Install the Service File

The source service template is kept in [`internal/systemd/bitwarden-ssh-agent-wrapper.service`](https://github.com/YewFence/bw-ssh-agent-notifier/blob/main/internal/systemd/bitwarden-ssh-agent-wrapper.service). Use `bwsshntfr systemd print-user-service` to generate the service file. It writes the current executable path into `ExecStart` and can write the selected upstream socket into the service environment.

```bash
mkdir -p "$HOME/.config/systemd/user"
bwsshntfr systemd print-user-service > "$HOME/.config/systemd/user/bw-ssh-agent-notifier.service"
```

Enable and start the generated user service.

```bash
systemctl --user daemon-reload
systemctl --user enable --now bw-ssh-agent-notifier.service
```

Check logs with the user journal.

```bash
journalctl --user -u bw-ssh-agent-notifier.service -f
```

## Custom Service File

For advanced users, you can copy [the service template](https://github.com/YewFence/bw-ssh-agent-notifier/blob/main/internal/systemd/bitwarden-ssh-agent-wrapper.service) to your config directory and manually edit it. The following is a simple example.

### Mise Installed Binary

When `bwsshntfr` is installed through mise, the service file can use mise like this. This works when mise is available in the service environment's `PATH`.

```ini
ExecStart=mise x -- bwsshntfr
```

## Use the Wrapper Socket

After the service is running, point SSH clients at the wrapper socket.

```bash
export SSH_AUTH_SOCK="$XDG_RUNTIME_DIR/bitwarden-ssh-agent-wrapper.sock"
```
