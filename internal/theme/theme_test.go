package theme

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveBuiltins(t *testing.T) {
	for _, name := range PaletteNames() {
		if _, ok := Resolve(name); !ok {
			t.Errorf("builtin %q not resolvable", name)
		}
	}
	if _, ok := Resolve("no-such-theme"); ok {
		t.Error("unknown theme should not resolve")
	}
	if got := ByName("no-such-theme").Palette.Name; got != GruvboxDark.Name {
		t.Errorf("ByName fallback = %q, want %q", got, GruvboxDark.Name)
	}
}

func TestNamesIncludesBuiltinsAndUsers(t *testing.T) {
	// seed the user registry without touching the filesystem
	userMu.Lock()
	userPalettes = map[string]Palette{"my-theme": {Name: "my-theme", BG: "#123456"}}
	userLoaded = true
	userMu.Unlock()
	t.Cleanup(func() {
		userMu.Lock()
		userPalettes = nil
		userLoaded = false
		userMu.Unlock()
	})

	names := Names()
	found := false
	for _, n := range names {
		if n == "my-theme" {
			found = true
		}
	}
	if !found {
		t.Errorf("Names() missing user theme: %v", names)
	}
	if p, ok := Resolve("my-theme"); !ok || p.BG != "#123456" {
		t.Errorf("Resolve(my-theme) = %+v, %v", p, ok)
	}
}

func TestLoadUserThemes(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("ocean.yml", "name: ocean\nbg: \"#001122\"\nfg: \"#aaccee\"\nred: \"#ff0000\"\n")
	write("broken.yml", "not: [valid\n")

	if err := LoadUserThemes(dir); err != nil {
		t.Fatalf("LoadUserThemes: %v", err)
	}
	p, ok := Resolve("ocean")
	if !ok {
		t.Fatal("user theme ocean not loaded")
	}
	if p.BG != "#001122" || p.Red != "#ff0000" {
		t.Errorf("ocean palette colors wrong: %+v", p)
	}
	if p.Green != GruvboxDark.Green {
		t.Errorf("unset slot should inherit the default, got %q", p.Green)
	}
	if _, ok := Resolve("broken"); ok {
		t.Error("unparsable theme file must be skipped")
	}
}

// TestNoDeadlock guards the reentrancy regression: Resolve must not
// block when it triggers the lazy user-theme load.
func TestNoDeadlock(t *testing.T) {
	done := make(chan struct{})
	go func() {
		_ = ByName("whatever")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("ByName deadlocked on the lazy user-theme load")
	}
}
