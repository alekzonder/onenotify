// Package config finds and loads onenotify configuration files.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

const systemConfig = "/etc/onenotify/config.yaml"

// NameRE is the pattern for channel names. Names become directory names.
var NameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]*$`)

// LogFile is the log file.
func (c *Config) LogFile() string { return filepath.Join(c.Log.Dir, "onenotify.log") }

// Channel returns the channel with the given name, or nil.
func (c *Config) Channel(name string) *Channel {
	for i := range c.Channels {
		if c.Channels[i].Name == name {
			return &c.Channels[i]
		}
	}
	return nil
}

// EnabledChannels returns the channels messages are queued for.
func (c *Config) EnabledChannels() []*Channel {
	var out []*Channel
	for i := range c.Channels {
		if c.Channels[i].Enabled {
			out = append(out, &c.Channels[i])
		}
	}
	return out
}

// Where a config file was found, as reported by Locate.
const (
	SourceFlag    = "--config"
	SourceEnv     = "ONENOTIFY_CONFIG"
	SourceDefault = "default location"
)

// Find returns the config file to use: the explicit path, $ONENOTIFY_CONFIG,
// /etc/onenotify/config.yaml, then $HOME/.config/onenotify/config.yaml.
func Find(flag string) (string, error) {
	path, _, err := Locate(flag)
	return path, err
}

// Locate is Find that also says where the config came from: SourceFlag,
// SourceEnv or SourceDefault.
func Locate(flag string) (path, source string, err error) {
	path, source = flag, SourceFlag
	if path == "" {
		path, source = os.Getenv("ONENOTIFY_CONFIG"), SourceEnv
	}
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", "", fmt.Errorf("config file %s: %w", path, err)
		}
		return path, source, nil
	}
	candidates := []string{systemConfig}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, ".config", "onenotify", "config.yaml"))
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path, SourceDefault, nil
		}
	}
	msg := "no config file found; looked in:"
	for _, path := range candidates {
		msg += "\n  " + path
	}
	return "", "", fmt.Errorf("%s\nuse --config <path> or set ONENOTIFY_CONFIG", msg)
}

// defaultConfig returns a config with every default applied. Root gets
// system directories, other users directories under $HOME. The default
// source is the event queue a onecheck of the same user writes to.
func defaultConfig() *Config {
	c := &Config{}
	c.Log.Level = "info"
	c.Log.Format = "text"
	c.Log.Rotate = Rotate{MaxSize: 10 << 20, Keep: 5, MaxAge: Duration(30 * 24 * time.Hour), Compress: true}
	c.Queue = Queue{TTL: Duration(24 * time.Hour), FailedTTL: Duration(7 * 24 * time.Hour)}
	c.Limits.Text = 64 << 10
	c.Run.Parallel = 4
	c.Defaults.Timeout = Duration(30 * time.Second)
	c.Modules.Repos = []string{"alekzonder/onenotify"}

	home, err := os.UserHomeDir()
	if os.Geteuid() == 0 || err != nil {
		c.Log.Dir = "/var/log/onenotify"
		c.Paths = Paths{
			Modules: "/usr/lib/onenotify/modules",
			Spool:   "/var/lib/onenotify/spool",
			Failed:  "/var/lib/onenotify/failed",
		}
		c.Sources = []Source{{Type: SourceOnecheck, Path: "/var/lib/onecheck/events"}}
		return c
	}
	state := filepath.Join(home, ".local", "state", "onenotify")
	c.Log.Dir = filepath.Join(state, "log")
	c.Paths = Paths{
		Modules: filepath.Join(home, ".local", "lib", "onenotify", "modules"),
		Spool:   filepath.Join(state, "spool"),
		Failed:  filepath.Join(state, "failed"),
	}
	c.Sources = []Source{{Type: SourceOnecheck, Path: filepath.Join(home, ".local", "state", "onecheck", "events")}}
	return c
}
