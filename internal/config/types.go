package config

import (
	"time"

	"gopkg.in/yaml.v3"

	"github.com/alekzonder/onenotify/internal/units"
)

// Size is a number of bytes written in config as "10MB".
type Size int64

// UnmarshalYAML parses a size literal.
func (s *Size) UnmarshalYAML(n *yaml.Node) error {
	v, err := units.ParseBytes(n.Value)
	if err != nil {
		return err
	}
	*s = Size(v)
	return nil
}

// Duration is a time span written in config as "30s", "1h" or "7d".
type Duration time.Duration

// UnmarshalYAML parses a duration literal.
func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	v, err := units.ParseDuration(n.Value)
	if err != nil {
		return err
	}
	*d = Duration(v * float64(time.Second))
	return nil
}

// Rotate holds the log rotation rules used by "onenotify logrotate".
type Rotate struct {
	MaxSize  Size     `yaml:"max_size"`
	Keep     int      `yaml:"keep"`
	MaxAge   Duration `yaml:"max_age"`
	Compress bool     `yaml:"compress"`
}

// Log configures logging.
type Log struct {
	Level  string `yaml:"level"`
	Dir    string `yaml:"dir"`
	Format string `yaml:"format"`
	Rotate Rotate `yaml:"rotate"`
}

// Paths holds the directories used by onenotify.
type Paths struct {
	Modules string `yaml:"modules"`
	Spool   string `yaml:"spool"`
	Failed  string `yaml:"failed"`
}

// SourceOnecheck is the only kind of source: a onecheck event queue.
const SourceOnecheck = "onecheck"

// Source is a directory onenotify takes events from.
type Source struct {
	Type string `yaml:"type"`
	Path string `yaml:"path"`
}

// Queue configures how long messages are kept.
type Queue struct {
	// TTL is the delivery deadline of a message that has none of its own.
	TTL Duration `yaml:"ttl"`
	// FailedTTL is how long undelivered messages are kept for inspection.
	FailedTTL Duration `yaml:"failed_ttl"`
}

// Limits bounds what a single message may hold.
type Limits struct {
	Text Size `yaml:"text"`
}

// Run configures how channels are served.
type Run struct {
	Parallel int `yaml:"parallel"`
}

// Defaults are applied to channels that do not set the value themselves.
type Defaults struct {
	Timeout Duration `yaml:"timeout"`
}

// Modules configures module management.
type Modules struct {
	Repos []string `yaml:"repos"`
}

// Config is a loaded onenotify configuration.
type Config struct {
	// Host is the host name put into every notification.
	Host string `yaml:"-"`
	// Path is the absolute path of the main config file.
	Path    string   `yaml:"-"`
	Log     Log      `yaml:"log"`
	Paths   Paths    `yaml:"paths"`
	Sources []Source `yaml:"-"`
	Include []string `yaml:"include"`
	// Includes are the include files that were read, in reading order.
	Includes []string  `yaml:"-"`
	Queue    Queue     `yaml:"queue"`
	Limits   Limits    `yaml:"limits"`
	Run      Run       `yaml:"run"`
	Defaults Defaults  `yaml:"defaults"`
	Modules  Modules   `yaml:"modules"`
	Channels []Channel `yaml:"-"`
}

// Channel is one configured destination of notifications.
type Channel struct {
	Name    string
	Module  string
	Enabled bool
	Timeout time.Duration
	// Params is the module input, with ${VAR} already expanded. It holds
	// secrets: never log it, print it or write it to the queue.
	Params map[string]any
	// Source is the file the channel was read from.
	Source string
	// Err is a problem that makes only this channel unusable, such as an
	// environment variable that is not set.
	Err error
}
