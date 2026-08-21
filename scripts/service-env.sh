#!/usr/bin/env bash
set -euo pipefail

env_file="$HOME/.config/bwsshntfr.env"

if [[ -f "$env_file" ]]; then
  if ! gum confirm "Overwrite the existing service env at $env_file?" --default=false; then
    echo "kept the existing $env_file"
    exit 0
  fi
fi

mkdir -p "$(dirname "$env_file")"

default_socket="$HOME/.var/app/com.bitwarden.desktop/data/.bitwarden-ssh-agent.sock"
socket="$(gum input --header "Bitwarden SSH agent socket path" --value "$default_socket")"

if gum confirm "Enable the signature gate? (confirm every signature request)" --default=false; then
  gate="confirm"
else
  gate="off"
fi

cat >"$env_file" <<EOF
BITWARDEN_SSH_AGENT_SOCKET=$socket
WRAPPER_GATE=$gate
EOF
echo "wrote $env_file"
