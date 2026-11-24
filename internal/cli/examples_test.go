package cli

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/deliver"
	"github.com/alekzonder/onenotify/internal/testmods"
)

// bundled lists the modules in cmd/: every directory cmd/onenotify-<name>.
func bundled(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join("..", "..", "cmd"))
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutPrefix(e.Name(), "onenotify-"); ok && e.IsDir() {
			names = append(names, name)
		}
	}
	return names
}

// TestExamplesLoadAndValidate keeps examples/ from drifting away from the
// code: the example config loads, every channel in it fits its real module,
// and every bundled module has an example channel.
func TestExamplesLoadAndValidate(t *testing.T) {
	names := bundled(t)
	if len(names) == 0 {
		t.Fatal("no bundled modules found")
	}
	dir, err := testmods.Build(names...)
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	for _, v := range []string{"TELEGRAM_TOKEN", "SMTP_PASSWORD", "NTFY_TOKEN"} {
		t.Setenv(v, "example-secret")
	}
	cfg, err := config.Load(filepath.Join("..", "..", "examples", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	cfg.Paths.Modules = dir
	if len(cfg.Includes) == 0 {
		t.Error("the example must show an include file")
	}
	used := map[string]bool{}
	for i := range cfg.Channels {
		ch := &cfg.Channels[i]
		used[ch.Module] = true
		if err := deliver.CheckChannel(context.Background(), cfg, ch); err != nil {
			t.Errorf("%s: channel %s: %v", ch.Source, ch.Name, err)
		}
	}
	for _, name := range names {
		if !used[name] {
			t.Errorf("bundled module %q has no channel in examples/config.yaml", name)
		}
	}
}
