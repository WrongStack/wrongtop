// Command wrongtop is a cross-platform terminal system monitor for
// macOS, Linux and Windows.
package main

import (
	"fmt"
	"os"

	"github.com/ersinkoc/wrongtop/internal/app"
	"github.com/ersinkoc/wrongtop/internal/config"
	"github.com/spf13/cobra"
)

// version is set at build time via -ldflags.
var version = "dev"

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

	root.Flags().StringP("config", "c", "",
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

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}
