package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/wrongstack/wrongtop/internal/collector"
	"github.com/wrongstack/wrongtop/internal/config"
	"github.com/wrongstack/wrongtop/internal/remote"
)

// stubEffects swaps every process-level seam for the duration of a test
// and restores the real functions afterwards.
func stubEffects(t *testing.T) (exitCodes *[]int, appCalls *[]string, remoteCalls *[]string, serveOpts *[]remote.Options) {
	t.Helper()
	var codes []int
	var apps, remotes []string
	var opts []remote.Options
	origExit, origApp, origRemote, origServe := osExit, runApp, runRemote, serveRemote
	osExit = func(code int) { codes = append(codes, code) }
	runApp = func(cfg *config.Config, cfgPath, version string) error {
		apps = append(apps, cfgPath+"|"+version+"|"+cfg.Theme)
		return nil
	}
	runRemote = func(cfg *config.Config, cfgPath, version string, stream *remote.Client, addr string) error {
		remotes = append(remotes, addr+"|"+stream.Hello.Protocol)
		stream.Close()
		return nil
	}
	serveRemote = func(ctx context.Context, o remote.Options) error {
		opts = append(opts, o)
		return nil
	}
	t.Cleanup(func() {
		osExit, runApp, runRemote, serveRemote = origExit, origApp, origRemote, origServe
	})
	return &codes, &apps, &remotes, &opts
}

// writeConfig drops a YAML config into a temp dir and returns its path.
func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// execute runs the command tree with args, capturing stdout+stderr.
func execute(t *testing.T, ctx context.Context, args ...string) (string, error) {
	t.Helper()
	root := newRootCmd()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(args)
	err := root.ExecuteContext(ctx)
	return out.String(), err
}

func TestResolveConfigPath(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("config", "", "")

	t.Setenv("WRONGTOP_CONFIG", "/env/config.yaml")
	if got := resolveConfigPath(cmd); got != "/env/config.yaml" {
		t.Errorf("empty flag should fall back to config.Path(): got %q", got)
	}
	if err := cmd.Flags().Set("config", "/flag/config.yaml"); err != nil {
		t.Fatal(err)
	}
	if got := resolveConfigPath(cmd); got != "/flag/config.yaml" {
		t.Errorf("explicit flag should win: got %q", got)
	}
}

func TestVersionAndSampleCommands(t *testing.T) {
	stubEffects(t)
	out, err := execute(t, context.Background(), "version")
	if err != nil || !strings.Contains(out, "wrongtop "+version) {
		t.Fatalf("version: err=%v out=%q", err, out)
	}
	out, err = execute(t, context.Background(), "config-sample")
	if err != nil || out != config.Sample {
		t.Fatalf("config-sample: err=%v, output differs from config.Sample", err)
	}
}

func TestRootRunsTheApp(t *testing.T) {
	_, apps, _, _ := stubEffects(t)
	cfg := writeConfig(t, "theme: dracula\n")
	if _, err := execute(t, context.Background(), "-c", cfg); err != nil {
		t.Fatal(err)
	}
	if len(*apps) != 1 || (*apps)[0] != cfg+"|"+version+"|dracula" {
		t.Errorf("app not started with the loaded config: %v", *apps)
	}
}

// A directory as the config path makes config.Load fail (read error that
// is not IsNotExist) — every subcommand must surface that error.
func TestConfigLoadErrorsPropagate(t *testing.T) {
	stubEffects(t)
	dir := t.TempDir()
	for _, args := range [][]string{
		{"-c", dir},
		{"serve", "-c", dir, "--token", "x"},
		{"connect", "127.0.0.1:1", "-c", dir},
		{"dump", "-c", dir},
	} {
		if _, err := execute(t, context.Background(), args...); err == nil {
			t.Errorf("%v: expected config error", args)
		}
	}
}

func TestServePassesOptionsThrough(t *testing.T) {
	_, _, _, opts := stubEffects(t)
	cfg := writeConfig(t, "refresh: 2s\n")
	_, err := execute(t, context.Background(), "serve", "-c", cfg, "--listen", "127.0.0.1:9", "--token", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if len(*opts) != 1 {
		t.Fatalf("serve called %d times", len(*opts))
	}
	o := (*opts)[0]
	if o.Listen != "127.0.0.1:9" || o.Token != "secret" || o.Refresh.Seconds() != 2 || o.Version != version {
		t.Errorf("options not passed through: %+v", o)
	}
}

func TestConnectDialFailure(t *testing.T) {
	_, _, remotes, _ := stubEffects(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close() // nothing listens here any more: connection refused
	if _, err := execute(t, context.Background(), "connect", addr); err == nil {
		t.Fatal("expected dial error")
	}
	if len(*remotes) != 0 {
		t.Error("remote TUI must not start after a failed dial")
	}
}

// fakeServer speaks just enough of the protocol for Dial to succeed: it
// reads the auth frame and answers with a Hello.
func fakeServer(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer func() { _ = conn.Close() }()
		var auth remote.Auth
		if err := remote.ReadFrame(conn, remote.MaxFrame, &auth); err != nil {
			return
		}
		_ = remote.WriteFrame(conn, remote.Hello{Protocol: remote.Protocol, Version: "fake", RefreshMS: 1000})
		_, _ = io.Copy(io.Discard, conn) // hold the line until the client hangs up
	}()
	return ln.Addr().String()
}

func TestConnectStartsRemoteTUI(t *testing.T) {
	_, _, remotes, _ := stubEffects(t)
	addr := fakeServer(t)
	if _, err := execute(t, context.Background(), "connect", addr, "--token", "t"); err != nil {
		t.Fatal(err)
	}
	if len(*remotes) != 1 || (*remotes)[0] != addr+"|"+remote.Protocol {
		t.Errorf("remote TUI not started against the dialed server: %v", *remotes)
	}
}

func TestDumpPrintsSnapshots(t *testing.T) {
	stubEffects(t)
	cfg := writeConfig(t, "refresh: 250ms\n")
	out, err := execute(t, context.Background(), "dump", "-c", cfg, "--count", "2")
	if err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(strings.NewReader(out))
	var n int
	for dec.More() {
		var snap collector.Snapshot
		if err := dec.Decode(&snap); err != nil {
			t.Fatalf("line %d: %v", n+1, err)
		}
		if snap.Time.IsZero() {
			t.Errorf("line %d: zero timestamp", n+1)
		}
		n++
	}
	if n != 2 {
		t.Errorf("got %d snapshots, want 2", n)
	}
}

// A cancelled context ends an open-ended dump after the first snapshot.
func TestDumpStopsOnCancelledContext(t *testing.T) {
	stubEffects(t)
	cfg := writeConfig(t, "refresh: 10s\n") // long refresh: only ctx can end this quickly
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	out, err := execute(t, ctx, "dump", "-c", cfg, "--count", "0")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(strings.TrimSpace(out), "\n") != 0 || !strings.HasPrefix(out, "{") {
		t.Errorf("expected exactly one JSON line, got %q", out)
	}
}

type failWriter struct{}

func (failWriter) Write([]byte) (int, error) { return 0, errors.New("stdout closed") }

func TestDumpReportsWriteErrors(t *testing.T) {
	stubEffects(t)
	cfg := writeConfig(t, "refresh: 250ms\n")
	root := newRootCmd()
	root.SetOut(failWriter{})
	root.SetErr(io.Discard)
	root.SetArgs([]string{"dump", "-c", cfg})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "stdout closed") {
		t.Fatalf("expected the encoder's write error, got %v", err)
	}
}

func TestMainExitsNonZeroOnError(t *testing.T) {
	codes, _, _, _ := stubEffects(t)
	origArgs := os.Args
	t.Cleanup(func() { os.Args = origArgs })

	os.Args = []string{"wrongtop", "version"}
	main()
	if len(*codes) != 0 {
		t.Fatalf("successful command must not exit: %v", *codes)
	}

	os.Args = []string{"wrongtop", "no-such-command"}
	main()
	if len(*codes) != 1 || (*codes)[0] != 1 {
		t.Fatalf("failing command must exit 1: %v", *codes)
	}
}
