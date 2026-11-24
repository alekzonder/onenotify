package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/alekzonder/onenotify/internal/config"
)

const pathsLong = `Show the full path of everything onenotify reads and writes for this config.

  config    the config file, and how it was found
  include   every include file that was read
  log       the log file
  spool     the queues: one directory per channel with the messages waiting
  failed    messages that expired or were rejected, kept for inspection
  source    a directory events are taken from (the onecheck event queue)
  modules   where modules are looked for
  binary    the onenotify executable itself

A path that does not exist yet is marked "(missing)"; onenotify creates the
log, spool and failed paths when they are first needed, and onecheck creates
its event queue on its first run. Nothing is created or changed by this
command, and modules do not have to be installed.

If the config is found but cannot be loaded, its path is still printed and
the command exits with 2.`

// pathEntry is one path in the JSON output of "onenotify paths".
type pathEntry struct {
	Path   string `json:"path"`
	Exists bool   `json:"exists"`
	Source string `json:"source,omitempty"`
}

// pathsReport is the JSON output of "onenotify paths".
type pathsReport struct {
	Host     string      `json:"host,omitempty"`
	Config   pathEntry   `json:"config"`
	Includes []pathEntry `json:"includes"`
	Log      *pathEntry  `json:"log,omitempty"`
	Spool    *pathEntry  `json:"spool,omitempty"`
	Failed   *pathEntry  `json:"failed,omitempty"`
	Sources  []pathEntry `json:"sources"`
	Modules  *pathEntry  `json:"modules,omitempty"`
	Binary   pathEntry   `json:"binary"`
}

func entry(path string) pathEntry {
	_, err := os.Stat(path)
	return pathEntry{Path: path, Exists: err == nil}
}

func entryPtr(path string) *pathEntry {
	e := entry(path)
	return &e
}

// binaryPath is the real location of the running executable.
func binaryPath() string {
	exe, err := os.Executable()
	if err != nil {
		return os.Args[0]
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		return real
	}
	return exe
}

func (a *app) pathsCmd() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:     "paths",
		Short:   "Show the config, log, spool, failed, source and modules paths",
		Long:    pathsLong,
		Example: "  onenotify paths\n  onenotify paths -c ./config.yaml --json",
		Args:    cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			return a.paths(asJSON)
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "print the paths as JSON")
	return cmd
}

func (a *app) paths(asJSON bool) error {
	path, source, err := config.Locate(a.configPath)
	if err != nil {
		return err
	}
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	report := pathsReport{Config: entry(path), Includes: []pathEntry{}, Sources: []pathEntry{}, Binary: entry(binaryPath())}
	report.Config.Source = source

	cfg, loadErr := config.Load(path)
	if loadErr == nil {
		report.Host = cfg.Host
		for _, inc := range cfg.Includes {
			report.Includes = append(report.Includes, entry(inc))
		}
		report.Log = entryPtr(cfg.LogFile())
		report.Spool = entryPtr(cfg.Paths.Spool)
		report.Failed = entryPtr(cfg.Paths.Failed)
		for _, src := range cfg.Sources {
			report.Sources = append(report.Sources, entry(src.Path))
		}
		report.Modules = entryPtr(cfg.Paths.Modules)
	}

	if asJSON {
		enc := json.NewEncoder(a.stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
		return loadErr
	}
	a.printPaths(report)
	return loadErr
}

func (a *app) printPaths(r pathsReport) {
	row := func(label string, e *pathEntry) {
		if e == nil {
			return
		}
		note := ""
		switch {
		case !e.Exists:
			note = "  (missing)"
		case e.Source != "":
			note = "  (from " + e.Source + ")"
		}
		fmt.Fprintf(a.stdout, "%-9s %s%s\n", label, e.Path, note)
	}
	row("config", &r.Config)
	if r.Host != "" {
		fmt.Fprintf(a.stdout, "%-9s %s\n", "host", r.Host)
	}
	for i := range r.Includes {
		row("include", &r.Includes[i])
	}
	row("log", r.Log)
	row("spool", r.Spool)
	row("failed", r.Failed)
	for i := range r.Sources {
		row("source", &r.Sources[i])
	}
	row("modules", r.Modules)
	row("binary", &r.Binary)
}
