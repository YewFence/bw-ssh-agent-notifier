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
  echo "Configure command prefixes for automatic scope selection."
  echo "Examples: bd sync, jj git push. Press Enter on an empty line when finished."
  command_groups=()
  while true; do
    group="$(gum input --header "Command prefix #$(( ${#command_groups[@]} + 1 )) (empty to finish)" --placeholder "bd sync")"
    if [[ -z "$group" ]]; then
      break
    fi
    if [[ "$group" == *,* ]]; then
      gum style --foreground 214 "Command prefixes cannot contain commas: $group"
      continue
    fi
    command_groups+=("$group")
  done
  command_groups_value="$(IFS=,; printf '%s' "${command_groups[*]-}")"
else
  gate="off"
  command_groups_value=""
fi

escape_env_value() {
  local value="$1"
  value=${value//\\/\\\\}
  value=${value//\"/\\\"}
  printf '%s' "$value"
}

{
  printf 'BITWARDEN_SSH_AGENT_SOCKET="%s"\n' "$(escape_env_value "$socket")"
  printf 'WRAPPER_GATE=%s\n' "$gate"
  if [[ -n "$command_groups_value" ]]; then
    printf 'WRAPPER_GATE_COMMAND_GROUPS="%s"\n' "$(escape_env_value "$command_groups_value")"
  fi
} >"$env_file"
echo "wrote $env_file"
