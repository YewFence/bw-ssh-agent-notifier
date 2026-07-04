package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

func NewRootCommand(version string) *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "bwsshntfr",
		Short: "simple wrapper for notify who use bw ssh agent on linux desktop",
		Long:  "simple wrapper for notify who use bw ssh agent on linux desktop.",
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := fmt.Fprintln(cmd.OutOrStdout(), "Hello from bwsshntfr")
			return err
		},
	}
	rootCmd.AddCommand(newVersionCommand(version))
	return rootCmd
}

func Execute(version string) {
	rootCmd := NewRootCommand(version)
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
