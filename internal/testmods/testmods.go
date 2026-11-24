// Package testmods builds module executables for tests: the bundled modules
// and a fake one that misbehaves on request. It is only imported from _test
// files.
package testmods

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func root() string {
	_, self, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(self), "..", "..")
}

func build(dir, name, pkg string) error {
	cmd := exec.Command("go", "build", "-o", filepath.Join(dir, "onenotify-"+name), pkg)
	cmd.Dir = root()
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("build module %s: %w\n%s", name, err, out)
	}
	return nil
}

// Build compiles the named bundled modules (e.g. "telegram", "ntfy") into a
// new temporary directory and returns it. The caller removes the directory.
func Build(names ...string) (string, error) {
	dir, err := os.MkdirTemp("", "onenotify-testmods")
	if err != nil {
		return "", err
	}
	for _, name := range names {
		if err := build(dir, name, "./cmd/onenotify-"+name); err != nil {
			os.RemoveAll(dir)
			return "", err
		}
	}
	return dir, nil
}

// BuildFake compiles the fake module as "onenotify-fake" into a new temporary
// directory and returns the directory. See fakemod for what it can do.
func BuildFake() (string, error) {
	dir, err := os.MkdirTemp("", "onenotify-fakemod")
	if err != nil {
		return "", err
	}
	if err := build(dir, "fake", "./internal/testmods/fakemod"); err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}
