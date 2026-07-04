# bwsshntfr

[![Release](https://img.shields.io/github/v/release/YewFence/bw-ssh-agent-notifier?sort=semver)](https://github.com/YewFence/bw-ssh-agent-notifier/releases)
[![Docs](https://img.shields.io/badge/docs-online-blue)](https://YewFence.github.io/bw-ssh-agent-notifier/)
[![License](https://img.shields.io/github/license/YewFence/bw-ssh-agent-notifier)](LICENSE)

simple wrapper for notify who use bw ssh agent on linux desktop

> [!NOTE]
> This project is in an early development stage. Core features may be missing, and backward compatibility is not guaranteed.

## Quick Start

### Installation

#### Mise

```bash
mise use --global github:YewFence/bw-ssh-agent-notifier
```

#### Go

```bash
go install github.com/YewFence/bw-ssh-agent-notifier/cmd/bwsshntfr@latest
```

#### Build and install from source locally

```bash
git clone https://github.com/YewFence/bw-ssh-agent-notifier.git
cd bw-ssh-agent-notifier
mise trust
mise install
mise run cli:install
```

### Usage

```bash
bwsshntfr
bwsshntfr version
```

## Documentation

See the [documentation site](https://YewFence.github.io/bw-ssh-agent-notifier) for more information.

## Contributing

If you have suggestions or find a bug, please [open an issue](https://github.com/YewFence/bw-ssh-agent-notifier/issues).

Pull requests are welcome. See the [Contributing Guide](CONTRIBUTING.md).

## License

[MIT License](LICENSE)
