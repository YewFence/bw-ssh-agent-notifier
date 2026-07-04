package cli

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/YewFence/bw-ssh-agent-notifier/internal/config"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/notify"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/process"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/proxy"
	"github.com/YewFence/bw-ssh-agent-notifier/internal/systemd"
	"github.com/spf13/cobra"
)

type runFunc func(context.Context, config.Config, *slog.Logger) error

func NewRootCommand(version string) *cobra.Command {
	return newRootCommand(version, runProxy)
}

func newRootCommand(version string, runner runFunc) *cobra.Command {
	cfg, envErr := config.FromEnv()
	rootCmd := &cobra.Command{
		Use:          "bwsshntfr",
		Short:        "Notify which process uses Bitwarden SSH agent",
		Long:         "Notify which process uses Bitwarden SSH agent on Linux desktop.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithConfig(cmd, envErr, cfg, runner)
		},
	}

	addConfigFlags(rootCmd, &cfg)
	rootCmd.AddCommand(newRunCommand(&cfg, &envErr, runner))
	rootCmd.AddCommand(newDoctorCommand(&cfg, &envErr))
	rootCmd.AddCommand(newNotifyCommand(&cfg, &envErr))
	rootCmd.AddCommand(newDebugCommand(&cfg, &envErr))
	rootCmd.AddCommand(newSystemdCommand(&cfg, &envErr))
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

func addConfigFlags(cmd *cobra.Command, cfg *config.Config) {
	flags := cmd.PersistentFlags()
	flags.StringVar(&cfg.ListenSocket, "listen", cfg.ListenSocket, "wrapper Unix socket path (env: WRAPPER_SSH_AGENT_SOCKET)")
	flags.StringVar(&cfg.UpstreamSocket, "upstream", cfg.UpstreamSocket, "Bitwarden SSH agent Unix socket path (env: BITWARDEN_SSH_AGENT_SOCKET)")
	flags.StringVar(&cfg.NotifyBackend, "notify", cfg.NotifyBackend, "notification backend: dbus or off (env: WRAPPER_NOTIFY_BACKEND)")
	flags.DurationVar(&cfg.NotifyCallTimeout, "notify-call-timeout", cfg.NotifyCallTimeout, "notification D-Bus call timeout (env: WRAPPER_NOTIFY_CALL_TIMEOUT)")
	flags.DurationVar(&cfg.NotifyExpireTimeout, "notify-expire-timeout", cfg.NotifyExpireTimeout, "notification display timeout (env: WRAPPER_NOTIFY_EXPIRE_TIMEOUT)")
	flags.BoolVar(&cfg.NotifyFullTree, "notify-full-process-tree", cfg.NotifyFullTree, "show the full process tree in notifications (env: WRAPPER_NOTIFY_FULL_PROCESS_TREE)")
	flags.IntVar(&cfg.ParentDepth, "parent-depth", cfg.ParentDepth, "parent process depth to inspect (env: WRAPPER_PARENT_DEPTH)")
	flags.StringVar(&cfg.LogLevel, "log-level", cfg.LogLevel, "log level: debug, info, warn, error")
	flags.StringVar(&cfg.LogFormat, "log-format", cfg.LogFormat, "log format: text or json")
}

func newRunCommand(cfg *config.Config, envErr *error, runner runFunc) *cobra.Command {
	return &cobra.Command{
		Use:   "run",
		Short: "Run the SSH agent wrapper",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithConfig(cmd, *envErr, *cfg, runner)
		},
	}
}

func runWithConfig(cmd *cobra.Command, envErr error, cfg config.Config, runner runFunc) error {
	if envErr != nil {
		return envErr
	}
	if err := cfg.Validate(); err != nil {
		return err
	}
	return runner(cmd.Context(), cfg, newLogger(cmd.OutOrStdout(), cfg))
}

func runProxy(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	var notifier notify.Notifier
	notifier = notify.Noop()
	if cfg.NotifyBackend == "dbus" {
		notifier = notify.DBusNotifier{CallTimeout: cfg.NotifyCallTimeout, ExpireTimeout: cfg.NotifyExpireTimeout}
	}
	return proxy.Server{Config: cfg, Notifier: notifier, Logger: logger}.Run(ctx)
}

func newDoctorCommand(cfg *config.Config, envErr *error) *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the runtime environment",
		RunE: func(cmd *cobra.Command, args []string) error {
			if *envErr != nil {
				return *envErr
			}
			return runDoctor(cmd, *cfg)
		},
	}
}

func runDoctor(cmd *cobra.Command, cfg config.Config) error {
	out := cmd.OutOrStdout()
	allOK := true
	allOK = printCheck(out, allOK, "XDG_RUNTIME_DIR", dirExists(os.Getenv("XDG_RUNTIME_DIR")))
	allOK = printCheck(out, allOK, "wrapper socket parent", dirWritable(socketParent(cfg.ListenSocket)))
	allOK = printCheck(out, allOK, "Bitwarden upstream socket", socketExists(cfg.UpstreamSocket))
	allOK = printCheck(out, allOK, "session D-Bus", canNotify(cmd.Context(), cfg))
	allOK = printCheck(out, allOK, "/proc process info", canInspectSelf(cfg.ParentDepth))
	if !allOK {
		return fmt.Errorf("one or more checks failed")
	}
	return nil
}

func newNotifyCommand(cfg *config.Config, envErr *error) *cobra.Command {
	notifyCmd := &cobra.Command{
		Use:   "notify",
		Short: "Notification commands",
	}
	var summary string
	var body string
	testCmd := &cobra.Command{
		Use:   "test",
		Short: "Send a test notification",
		RunE: func(cmd *cobra.Command, args []string) error {
			if *envErr != nil {
				return *envErr
			}
			if cfg.NotifyBackend == "off" {
				return fmt.Errorf("notification backend is off")
			}
			notifier := notify.DBusNotifier{CallTimeout: cfg.NotifyCallTimeout, ExpireTimeout: cfg.NotifyExpireTimeout}
			return notifier.Send(cmd.Context(), notify.Notification{Summary: summary, Body: body})
		},
	}
	testCmd.Flags().StringVar(&summary, "summary", "SSH agent request", "notification summary")
	testCmd.Flags().StringVar(&body, "body", "test notification", "notification body")
	testCmd.Flags().DurationVar(&cfg.NotifyExpireTimeout, "expire-timeout", cfg.NotifyExpireTimeout, "notification display timeout")
	notifyCmd.AddCommand(testCmd)
	return notifyCmd
}

func newDebugCommand(cfg *config.Config, envErr *error) *cobra.Command {
	debugCmd := &cobra.Command{
		Use:   "debug",
		Short: "Debug commands",
	}
	debugCmd.AddCommand(&cobra.Command{
		Use:   "inspect-pid PID",
		Short: "Print process information",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if *envErr != nil {
				return *envErr
			}
			pid, err := strconv.Atoi(args[0])
			if err != nil {
				return err
			}
			info, err := process.Inspect(pid, cfg.ParentDepth)
			if err != nil {
				return err
			}
			_, err = fmt.Fprintf(cmd.OutOrStdout(), "pid=%d\nuid=%d\ngid=%d\nexe=%s\ncmdline=%s\nparent_chain=%s\n",
				info.PID,
				info.UID,
				info.GID,
				info.Exe,
				process.CommandLine(info.Cmdline),
				process.ParentChain(info.Parents),
			)
			return err
		},
	})
	return debugCmd
}

func newSystemdCommand(cfg *config.Config, envErr *error) *cobra.Command {
	systemdCmd := &cobra.Command{
		Use:   "systemd",
		Short: "Systemd helper commands",
	}
	systemdCmd.AddCommand(&cobra.Command{
		Use:   "print-user-service",
		Short: "Print a systemd user service",
		RunE: func(cmd *cobra.Command, args []string) error {
			if *envErr != nil {
				return *envErr
			}
			cliPath, err := systemd.CurrentExecutablePath()
			if err != nil {
				return err
			}
			service, err := systemd.UserService(cliPath, cfg.UpstreamSocket)
			if err != nil {
				return err
			}
			_, err = fmt.Fprint(cmd.OutOrStdout(), service)
			return err
		},
	})
	return systemdCmd
}

func printCheck(out io.Writer, current bool, name string, err error) bool {
	if err != nil {
		if _, writeErr := fmt.Fprintf(out, "FAIL %s %v\n", name, err); writeErr != nil {
			return false
		}
		return false
	}
	if _, writeErr := fmt.Fprintf(out, "OK %s\n", name); writeErr != nil {
		return false
	}
	return current
}

func dirExists(path string) error {
	if path == "" {
		return fmt.Errorf("is not set")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("is not a directory")
	}
	return nil
}

func socketParent(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Dir(path)
}

func dirWritable(path string) error {
	if path == "" {
		return fmt.Errorf("is not set")
	}
	if err := dirExists(path); err != nil {
		return err
	}
	file, err := os.CreateTemp(path, ".bwsshntfr-write-check-*")
	if err != nil {
		return err
	}
	name := file.Name()
	if err := file.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	return os.Remove(name)
}

func socketExists(path string) error {
	if path == "" {
		return fmt.Errorf("is not set")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode()&os.ModeSocket == 0 {
		return fmt.Errorf("is not a socket")
	}
	return nil
}

func canNotify(ctx context.Context, cfg config.Config) error {
	if cfg.NotifyBackend == "off" {
		return nil
	}
	return notify.CheckDBus(ctx, cfg.NotifyCallTimeout)
}

func canInspectSelf(parentDepth int) error {
	_, err := process.Inspect(os.Getpid(), parentDepth)
	return err
}

func newLogger(out io.Writer, cfg config.Config) *slog.Logger {
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(out, opts))
	}
	return slog.New(slog.NewTextHandler(out, opts))
}
