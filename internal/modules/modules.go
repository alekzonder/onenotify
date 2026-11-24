// Package modules finds delivery modules on disk and runs them.
package modules

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/alekzonder/onenotify/pkg/module"
)

// The range of module protocol versions this onenotify can talk to.
const (
	MinProtocol = 1
	MaxProtocol = 1
)

// Prefix is the file name prefix of every module executable.
const Prefix = "onenotify-"

var describeTimeout = 5 * time.Second

// ErrIncompatible means a module speaks a protocol version outside the
// supported range.
var ErrIncompatible = errors.New("incompatible protocol")

// Info is a module found on disk together with its self-description.
type Info struct {
	module.Description
	Path string
}

// Compatible reports whether onenotify supports the module's protocol version.
func (i *Info) Compatible() bool {
	return i.Protocol >= MinProtocol && i.Protocol <= MaxProtocol
}

// CompatError explains why an incompatible module cannot be used.
func (i *Info) CompatError() error {
	if i.Compatible() {
		return nil
	}
	return fmt.Errorf("%w: module %q speaks protocol %d, this onenotify supports %d..%d; update the module or onenotify",
		ErrIncompatible, i.Name, i.Protocol, MinProtocol, MaxProtocol)
}

// Path returns where the module with the given name lives in dir.
func Path(dir, name string) string {
	return filepath.Join(dir, Prefix+name)
}

// List describes every module executable in dir. A missing directory is an
// empty list. Files that are not usable modules are reported in errs.
func List(ctx context.Context, dir string) (infos []*Info, errs []error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			errs = append(errs, err)
		}
		return nil, errs
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, Prefix) || strings.HasSuffix(name, ".tmp") {
			continue
		}
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil || !fi.Mode().IsRegular() || fi.Mode().Perm()&0o111 == 0 {
			continue
		}
		info, err := Describe(ctx, filepath.Join(dir, name))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if want := strings.TrimPrefix(name, Prefix); info.Name != want {
			errs = append(errs, fmt.Errorf("%s: the module calls itself %q, the file name says %q",
				filepath.Join(dir, name), info.Name, want))
			continue
		}
		infos = append(infos, info)
	}
	sort.Slice(infos, func(a, b int) bool { return infos[a].Name < infos[b].Name })
	return infos, errs
}

// ErrNotInstalled means there is no module with that name in the directory.
var ErrNotInstalled = errors.New("module is not installed")

// Load describes the module with the given name and checks that it can be
// used: it exists, is the module its file name says, and speaks a supported
// protocol version.
func Load(ctx context.Context, dir, name string) (*Info, error) {
	path := Path(dir, name)
	if _, err := os.Stat(path); err != nil {
		return nil, fmt.Errorf("%w: %s (looked for %s)", ErrNotInstalled, name, path)
	}
	info, err := Describe(ctx, path)
	if err != nil {
		return nil, err
	}
	if info.Name != name {
		return nil, fmt.Errorf("%s: the module calls itself %q, the file name says %q", path, info.Name, name)
	}
	if err := info.CompatError(); err != nil {
		return nil, err
	}
	return info, nil
}
