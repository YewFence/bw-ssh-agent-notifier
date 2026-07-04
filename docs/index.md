---
layout: home

hero:
  name: 'bw-ssh-agent-notifier'
  text: 'Show which local process is using the Bitwarden SSH agent'
  tagline: 'Notify when local processes access the Bitwarden SSH agent.'
  actions:
    - theme: brand
      text: GitHub Repo
      link: https://github.com/YewFence/bw-ssh-agent-notifier
    - theme: alt
      text: Troubleshooting
      link: /guide/troubleshooting

features:
  - title: Installation
    details: See the repository README for installation options, including mise and source builds.
  - title: Systemd user service
    details: Generate a user service with bwsshntfr systemd print-user-service and run it through systemd --user.
    link: /guide/systemd
  - title: Troubleshooting
    details: Diagnose upstream socket, D-Bus notification, stale socket, and service startup problems.
    link: /guide/troubleshooting
  - title: Configuration
    details: Reference every flag, environment variable, default path, notification backend, and logging option.
    link: /reference/configuration
  - title: Commands
    details: Use doctor, notify test, debug inspect-pid, systemd print-user-service, completion, and version.
    link: /reference/commands
  - title: Architecture
    details: Understand the local proxy model, process inspection path, notification flow, and project boundaries.
    link: /reference/architecture
  - title: Shell Completion
    details: Generate completion scripts for Bash, Zsh, Fish, and PowerShell.
    link: /guide/completion
---
