// Command wrongtop is a cross-platform terminal system monitor for
// macOS, Linux and Windows.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/spf13/cobra"
	"github.com/wrongstack/wrongtop/internal/app"
	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/remote"
)

// version is set at build time via -ldflags.
var version = "dev"

// subcommand flag storage
var (
	serveListen  string
	serveToken   string
	connectToken string
	dumpCount    int
)

// Process-level effects are indirected so the command tree can run end to
// end in tests without a terminal or a real exit.
var (
	osExit      = os.Exit
	runApp      = app.Run
	runRemote   = app.RunRemote
	serveRemote = remote.Serve
)

// resolveConfigPath returns the -c flag value or the default location.
func resolveConfigPath(cmd *cobra.Command) string {
	path := cmd.Flag("config").Value.String()
	if path == "" {
		path = config.Path()
	}
	return path
}

// newRootCmd builds the full command tree.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:          "wrongtop",
		Short:        "A cross-platform terminal system monitor",
		Long:         "WrongTop is a terminal system monitor for macOS, Linux and Windows.\nIt watches CPU, memory, processes, disks, network and Docker containers.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(cmd)
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			return runApp(cfg, path, version)
		},
	}

	// persistent so serve/connect/dump inherit it too
	root.PersistentFlags().StringP("config", "c", "",
		"config file path (default: $WRONGTOP_CONFIG or ~/.config/wrongtop/config.yaml)")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprintln(cmd.OutOrStdout(), "wrongtop", version)
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "config-sample",
		Short: "Print an annotated sample config file",
		Run: func(cmd *cobra.Command, args []string) {
			_, _ = fmt.Fprint(cmd.OutOrStdout(), config.Sample)
		},
	})

	serve := &cobra.Command{
		Use:   "serve",
		Short: "Stream snapshots to wrongtop connect clients (read-only)",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(cmd)
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			return serveRemote(cmd.Context(), remote.Options{
				Listen:  serveListen,
				Token:   serveToken,
				Refresh: cfg.Refresh.D(),
				Version: version,
			})
		},
	}
	serve.Flags().StringVar(&serveListen, "listen", ":"+strconv.Itoa(remote.DefaultPort), "listen address")
	serve.Flags().StringVar(&serveToken, "token", os.Getenv("WRONGTOP_TOKEN"),
		"token clients must present (default: $WRONGTOP_TOKEN)")

	connect := &cobra.Command{
		Use:   "connect host:port",
		Short: "Watch a remote machine running wrongtop serve",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(cmd)
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
			defer cancel()
			stream, err := remote.Dial(ctx, args[0], connectToken, version)
			if err != nil {
				return err
			}
			return runRemote(cfg, path, version, stream, args[0])
		},
	}
	connect.Flags().StringVar(&connectToken, "token", os.Getenv("WRONGTOP_TOKEN"),
		"token the server expects (default: $WRONGTOP_TOKEN)")

	dump := &cobra.Command{
		Use:   "dump",
		Short: "Print snapshots as JSON lines to stdout",
		RunE: func(cmd *cobra.Command, args []string) error {
			path := resolveConfigPath(cmd)
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			ctx, cancel := signal.NotifyContext(cmd.Context(), os.Interrupt)
			defer cancel()
			coll := collector.New(cfg.Refresh.D())
			enc := json.NewEncoder(cmd.OutOrStdout())
			for i := 0; dumpCount <= 0 || i < dumpCount; i++ {
				if err := enc.Encode(coll.Collect(ctx)); err != nil {
					return err
				}
				select {
				case <-ctx.Done():
					return nil
				case <-time.After(cfg.Refresh.D()):
				}
			}
			return nil
		},
	}
	dump.Flags().IntVar(&dumpCount, "count", 1, "number of snapshots (0 = until interrupted)")

	root.AddCommand(serve, connect, dump)
	return root
}

func main() {
	if err := newRootCmd().Execute(); err != nil {
		osExit(1)
	}
}
