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

	"github.com/wrongstack/wrongtop/internal/app"
	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/remote"
	"github.com/spf13/cobra"
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

// resolveConfigPath returns the -c flag value or the default location.
func resolveConfigPath(cmd *cobra.Command) string {
	path := cmd.Flag("config").Value.String()
	if path == "" {
		path = config.Path()
	}
	return path
}

func main() {
	root := &cobra.Command{
		Use:          "wrongtop",
		Short:        "A cross-platform terminal system monitor",
		Long:         "WrongTop is a terminal system monitor for macOS, Linux and Windows.\nIt watches CPU, memory, processes, disks, network and Docker containers.",
		SilenceUsage: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := cmd.Flag("config").Value.String()
			if path == "" {
				path = config.Path()
			}
			cfg, err := config.Load(path)
			if err != nil {
				return err
			}
			return app.Run(cfg, path, version)
		},
	}

	// persistent so serve/connect/dump inherit it too
	root.PersistentFlags().StringP("config", "c", "",
		"config file path (default: $WRONGTOP_CONFIG or ~/.config/wrongtop/config.yaml)")

	root.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println("wrongtop", version)
		},
	})

	root.AddCommand(&cobra.Command{
		Use:   "config-sample",
		Short: "Print an annotated sample config file",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Print(config.Sample)
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
			return remote.Serve(context.Background(), remote.Options{
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
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			stream, err := remote.Dial(ctx, args[0], connectToken, version)
			if err != nil {
				return err
			}
			return app.RunRemote(cfg, path, version, stream, args[0])
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
			ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
			defer cancel()
			coll := collector.New(cfg.Refresh.D())
			enc := json.NewEncoder(os.Stdout)
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

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
