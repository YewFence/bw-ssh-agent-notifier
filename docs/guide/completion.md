# Shell Completion

bwsshntfr uses Cobra's native completion support and does not require an additional generator.

## Generate Completion Scripts

After building the binary, use the `completion` subcommand to generate completion scripts for Bash, Zsh, Fish, and PowerShell.

```bash
mise run build
./bin/bwsshntfr completion zsh > _bwsshntfr
./bin/bwsshntfr completion bash > bwsshntfr.bash
./bin/bwsshntfr completion fish > bwsshntfr.fish
./bin/bwsshntfr completion powershell > bwsshntfr.ps1
```

## Installation Examples

For Zsh, place the generated `_bwsshntfr` file in an existing directory from `$fpath`, or place it in a custom directory and add that directory in `~/.zshrc`.

```bash
mkdir -p ~/.zsh/completions
./bin/bwsshntfr completion zsh > ~/.zsh/completions/_bwsshntfr
```

```zsh
fpath=(~/.zsh/completions $fpath)
autoload -Uz compinit
compinit
```

For Bash, place the completion script in a local directory and source it manually, or let the system bash-completion directory manage it.

```bash
mkdir -p ~/.bash_completion.d
./bin/bwsshntfr completion bash > ~/.bash_completion.d/bwsshntfr.bash
source ~/.bash_completion.d/bwsshntfr.bash
```

For Fish, write the script directly to the user completion directory.

```bash
mkdir -p ~/.config/fish/completions
./bin/bwsshntfr completion fish > ~/.config/fish/completions/bwsshntfr.fish
```
