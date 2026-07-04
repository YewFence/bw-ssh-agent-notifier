package main

import "github.com/YewFence/bw-ssh-agent-notifier/internal/cli"

var version = "dev"

func main() {
	cli.Execute(version)
}
