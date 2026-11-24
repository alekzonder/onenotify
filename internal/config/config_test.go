package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func load(t *testing.T, content string) (*Config, error) {
	t.Helper()
	return Load(write(t, t.TempDir(), "config.yaml", content))
}

func mustLoad(t *testing.T, content string) *Config {
	t.Helper()
	cfg, err := load(t, content)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestLoadDefaults(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	hostname = func() (string, error) { return "web-01\n", nil }
	t.Cleanup(func() { hostname = os.Hostname })
	cfg := mustLoad(t, "")
	if cfg.Host != "web-01" {
		t.Errorf("host = %q", cfg.Host)
	}
	if cfg.Log.Level != "info" || cfg.Log.Format != "text" {
		t.Errorf("log = %+v", cfg.Log)
	}
	if time.Duration(cfg.Queue.TTL) != 24*time.Hour || time.Duration(cfg.Queue.FailedTTL) != 7*24*time.Hour {
		t.Errorf("queue = %+v", cfg.Queue)
	}
	if cfg.Limits.Text != 64<<10 || cfg.Run.Parallel != 4 || time.Duration(cfg.Defaults.Timeout) != 30*time.Second {
		t.Errorf("limits=%+v run=%+v defaults=%+v", cfg.Limits, cfg.Run, cfg.Defaults)
	}
	if len(cfg.Modules.Repos) != 1 || cfg.Modules.Repos[0] != "alekzonder/onenotify" {
		t.Errorf("repos = %v", cfg.Modules.Repos)
	}
	if len(cfg.Channels) != 0 {
		t.Errorf("channels = %v", cfg.Channels)
	}
	if os.Geteuid() != 0 {
		state := filepath.Join(home, ".local", "state", "onenotify")
		if cfg.Paths.Spool != filepath.Join(state, "spool") || cfg.Paths.Failed != filepath.Join(state, "failed") ||
			cfg.Paths.Modules != filepath.Join(home, ".local", "lib", "onenotify", "modules") ||
			cfg.LogFile() != filepath.Join(state, "log", "onenotify.log") {
			t.Errorf("paths = %+v log = %s", cfg.Paths, cfg.LogFile())
		}
		want := filepath.Join(home, ".local", "state", "onecheck", "events")
		if len(cfg.Sources) != 1 || cfg.Sources[0] != (Source{Type: "onecheck", Path: want}) {
			t.Errorf("sources = %+v", cfg.Sources)
		}
	}
}

// loadFull loads a config that sets every key, with one include file.
func loadFull(t *testing.T) (cfg *Config, dir, path string) {
	t.Helper()
	t.Setenv("TG_TOKEN", "123:abc")
	dir = t.TempDir()
	write(t, dir, "conf.d/mail.yaml", "channels:\n  - name: admin-mail\n    module: mail\n    enabled: false\n    params: {host: smtp.example.com}\n")
	path = write(t, dir, "config.yaml", `host: db-02
log: {level: debug, dir: log, format: json, rotate: {max_size: 1MB, keep: 2, max_age: 1d, compress: false}}
paths: {modules: mods, spool: /abs/spool, failed: failed}
sources:
  - {type: onecheck, path: events}
  - {type: onecheck, path: /var/other}
include: [conf.d/*.yaml]
queue: {ttl: 2h, failed_ttl: 3d}
limits: {text: 1KB}
run: {parallel: 2}
defaults: {timeout: 5s}
modules: {repos: [a/b]}
channels:
  - name: ops
    module: telegram
    timeout: 10s
    params:
      token: "${TG_TOKEN}"
      chat_id: "-100"
      literal: "$${TG_TOKEN} costs $5"
      nested: {list: ["x-${TG_TOKEN}"]}
  - name: phone
    module: ntfy
`)
	cfg, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, dir, path
}

func TestLoadFullSettings(t *testing.T) {
	cfg, dir, _ := loadFull(t)
	if cfg.Host != "db-02" || cfg.Log.Dir != filepath.Join(dir, "log") || cfg.Paths.Modules != filepath.Join(dir, "mods") ||
		cfg.Paths.Spool != "/abs/spool" || cfg.Paths.Failed != filepath.Join(dir, "failed") {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.Sources) != 2 || cfg.Sources[0].Path != filepath.Join(dir, "events") || cfg.Sources[1].Path != "/var/other" {
		t.Errorf("sources = %+v", cfg.Sources)
	}
	if time.Duration(cfg.Queue.TTL) != 2*time.Hour || cfg.Limits.Text != 1024 || cfg.Run.Parallel != 2 {
		t.Errorf("cfg = %+v", cfg)
	}
	if len(cfg.Includes) != 1 || cfg.Includes[0] != filepath.Join(dir, "conf.d", "mail.yaml") {
		t.Errorf("includes = %v", cfg.Includes)
	}
}

func TestLoadFullChannels(t *testing.T) {
	cfg, _, path := loadFull(t)
	if len(cfg.Channels) != 3 {
		t.Fatalf("channels = %+v", cfg.Channels)
	}
	ops := cfg.Channel("ops")
	if ops.Module != "telegram" || !ops.Enabled || ops.Timeout != 10*time.Second || ops.Err != nil || ops.Source != path {
		t.Errorf("ops = %+v", ops)
	}
	if ops.Params["token"] != "123:abc" || ops.Params["literal"] != "${TG_TOKEN} costs $5" ||
		ops.Params["nested"].(map[string]any)["list"].([]any)[0] != "x-123:abc" {
		t.Errorf("params = %v", ops.Params)
	}
	if phone := cfg.Channel("phone"); phone.Timeout != 5*time.Second || phone.Params != nil {
		t.Errorf("phone = %+v", phone)
	}
	if mail := cfg.Channel("admin-mail"); mail.Enabled || mail.Source != cfg.Includes[0] {
		t.Errorf("mail = %+v", mail)
	}
	if cfg.Channel("nope") != nil {
		t.Error("unknown channel must be nil")
	}
	if en := cfg.EnabledChannels(); len(en) != 2 || en[0].Name != "ops" || en[1].Name != "phone" {
		t.Errorf("enabled = %+v", en)
	}
}

func TestSourcesEmptyListDisablesIntake(t *testing.T) {
	if cfg := mustLoad(t, "sources: []\n"); len(cfg.Sources) != 0 {
		t.Errorf("sources = %+v", cfg.Sources)
	}
}

func TestUndefinedVariableBreaksOnlyThatChannel(t *testing.T) {
	cfg := mustLoad(t, "channels:\n  - {name: a, module: ntfy, params: {token: \"${ONENOTIFY_TEST_UNSET}\"}}\n  - {name: b, module: ntfy}\n")
	a, b := cfg.Channel("a"), cfg.Channel("b")
	if a.Err == nil || !strings.Contains(a.Err.Error(), "ONENOTIFY_TEST_UNSET") || a.Params != nil {
		t.Errorf("a = %+v", a)
	}
	if b.Err != nil {
		t.Errorf("b = %+v", b)
	}
	// A broken channel still gets messages queued: they wait for the fix.
	if len(cfg.EnabledChannels()) != 2 {
		t.Errorf("enabled = %+v", cfg.EnabledChannels())
	}
}

func TestVariablesAreExpandedOnlyInParams(t *testing.T) {
	t.Setenv("ONENOTIFY_TEST_HOST", "expanded")
	if cfg := mustLoad(t, "host: \"${ONENOTIFY_TEST_HOST}\"\n"); cfg.Host != "${ONENOTIFY_TEST_HOST}" {
		t.Errorf("host = %q", cfg.Host)
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]string{
		"unknown top-level key":   "hots: x\n",
		"unknown channel key":     "channels:\n  - {name: a, module: ntfy, parms: {}}\n",
		"channel is not a map":    "channels:\n  - just-a-string\n",
		"channel without name":    "channels:\n  - {module: ntfy}\n",
		"channel without module":  "channels:\n  - {name: a}\n",
		"name with a slash":       "channels:\n  - {name: a/b, module: ntfy}\n",
		"name starting with dot":  "channels:\n  - {name: .x, module: ntfy}\n",
		"reserved name":           "channels:\n  - {name: _unreadable, module: ntfy}\n",
		"upper-case name":         "channels:\n  - {name: Ops, module: ntfy}\n",
		"module with a slash":     "channels:\n  - {name: a, module: ../x}\n",
		"duplicate name":          "channels:\n  - {name: a, module: ntfy}\n  - {name: a, module: mail}\n",
		"zero channel timeout":    "channels:\n  - {name: a, module: ntfy, timeout: 0s}\n",
		"bad channel timeout":     "channels:\n  - {name: a, module: ntfy, timeout: soon}\n",
		"empty host":              "host: \"  \"\n",
		"multi-line host":         "host: \"a\\nb\"\n",
		"unknown source type":     "sources:\n  - {type: webhook, path: /x}\n",
		"source without path":     "sources:\n  - {type: onecheck}\n",
		"zero parallel":           "run: {parallel: 0}\n",
		"zero default timeout":    "defaults: {timeout: 0s}\n",
		"zero ttl":                "queue: {ttl: 0s}\n",
		"zero failed ttl":         "queue: {failed_ttl: 0s}\n",
		"zero text limit":         "limits: {text: 0}\n",
		"bad log level":           "log: {level: loud}\n",
		"bad log format":          "log: {format: xml}\n",
		"unterminated variable":   "channels:\n  - {name: a, module: ntfy, params: {t: \"${X\"}}\n",
		"not yaml":                "host: [unclosed\n",
		"unknown key in defaults": "defaults: {fail_after: 2}\n",
	}
	for name, content := range cases {
		cfg, err := load(t, content)
		if err == nil && name == "unterminated variable" {
			// A broken variable reference is a channel problem.
			if cfg.Channels[0].Err == nil {
				t.Errorf("%s: no error", name)
			}
			continue
		}
		if err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}

func TestUnknownChannelKeyNamesFileAndLine(t *testing.T) {
	_, err := load(t, "channels:\n  - name: a\n    module: ntfy\n    parms: {}\n")
	if err == nil || !strings.Contains(err.Error(), "config.yaml") || !strings.Contains(err.Error(), "line 4") || !strings.Contains(err.Error(), "parms") {
		t.Errorf("err = %v", err)
	}
}

func TestIncludeRules(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "conf.d/a.yaml", "channels:\n  - {name: a, module: ntfy}\n")
	path := write(t, dir, "config.yaml", "include: [conf.d/*.yaml]\nchannels:\n  - {name: a, module: mail}\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "duplicate channel name") || !strings.Contains(err.Error(), "a.yaml") {
		t.Errorf("duplicate across files: %v", err)
	}
	write(t, dir, "conf.d/a.yaml", "host: other\n")
	path = write(t, dir, "config.yaml", "include: [conf.d/*.yaml]\n")
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "may only contain") {
		t.Errorf("include with other keys: %v", err)
	}
}

func TestHostnameFailure(t *testing.T) {
	hostname = func() (string, error) { return "", errors.New("no") }
	t.Cleanup(func() { hostname = os.Hostname })
	if _, err := load(t, ""); err == nil || !strings.Contains(err.Error(), "host") {
		t.Errorf("err = %v", err)
	}
	if cfg := mustLoad(t, "host: given\n"); cfg.Host != "given" {
		t.Errorf("host = %q", cfg.Host)
	}
}

func TestLocate(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("ONENOTIFY_CONFIG", "")
	if _, _, err := Locate(""); err == nil && os.Geteuid() != 0 {
		if _, statErr := os.Stat(systemConfig); statErr != nil {
			t.Error("no config anywhere must be an error")
		}
	}
	inHome := write(t, home, ".config/onenotify/config.yaml", "")
	if _, statErr := os.Stat(systemConfig); statErr != nil {
		if path, source, err := Locate(""); err != nil || path != inHome || source != SourceDefault {
			t.Errorf("home: %q %q %v", path, source, err)
		}
	}
	env := write(t, t.TempDir(), "env.yaml", "")
	t.Setenv("ONENOTIFY_CONFIG", env)
	if path, source, err := Locate(""); err != nil || path != env || source != SourceEnv {
		t.Errorf("env: %q %q %v", path, source, err)
	}
	flag := write(t, t.TempDir(), "flag.yaml", "")
	if path, source, err := Locate(flag); err != nil || path != flag || source != SourceFlag {
		t.Errorf("flag: %q %q %v", path, source, err)
	}
	if _, _, err := Locate(filepath.Join(home, "absent.yaml")); err == nil {
		t.Error("a missing explicit config must be an error")
	}
}
