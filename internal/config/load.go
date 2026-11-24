package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// fileChannels is the part of a config file that holds channels; include
// files may contain nothing else.
type fileChannels struct {
	Channels []yaml.Node `yaml:"channels"`
}

// rawChannel mirrors the YAML shape of a channel.
type rawChannel struct {
	Name    string         `yaml:"name"`
	Module  string         `yaml:"module"`
	Enabled *bool          `yaml:"enabled"`
	Timeout *Duration      `yaml:"timeout"`
	Params  map[string]any `yaml:"params"`
}

// Load reads the config file and its includes. A returned error means the
// whole config is unusable. The one problem that makes only a channel
// unusable, an environment variable that is not set, is stored in Channel.Err.
func Load(path string) (*Config, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("cannot read config: %w", err)
	}
	cfg := defaultConfig()
	cfg.Path = abs
	main := struct {
		*Config      `yaml:",inline"`
		Host         *string     `yaml:"host"`
		Sources      *[]Source   `yaml:"sources"`
		ChannelNodes []yaml.Node `yaml:"channels"`
	}{Config: cfg}
	if err := decodeStrict(data, &main); err != nil {
		return nil, fmt.Errorf("%s: %w", abs, err)
	}
	if main.Sources != nil {
		// An explicit list, even an empty one, replaces the default.
		cfg.Sources = *main.Sources
	}
	if err := cfg.setHost(main.Host); err != nil {
		return nil, fmt.Errorf("%s: %w", abs, err)
	}
	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", abs, err)
	}
	cfg.resolvePaths()

	sources := map[string]string{}
	if err := cfg.addChannels(main.ChannelNodes, abs, sources); err != nil {
		return nil, err
	}
	files, err := cfg.includeFiles()
	if err != nil {
		return nil, err
	}
	cfg.Includes = files
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			return nil, fmt.Errorf("cannot read include: %w", err)
		}
		var inc fileChannels
		if err := decodeStrict(data, &inc); err != nil {
			return nil, fmt.Errorf("%s: %w (include files may only contain \"channels\")", file, err)
		}
		if err := cfg.addChannels(inc.Channels, file, sources); err != nil {
			return nil, err
		}
	}
	return cfg, nil
}

func decodeStrict(data []byte, out any) error {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	return nil
}

// hostname is replaced in tests.
var hostname = os.Hostname

// setHost takes the host name from the config or, without one, from the system.
func (c *Config) setHost(fromConfig *string) error {
	if fromConfig != nil {
		c.Host = strings.TrimSpace(*fromConfig)
		if c.Host == "" {
			return errors.New(`"host" must not be empty: every notification carries the host name`)
		}
	} else {
		name, err := hostname()
		if err != nil || strings.TrimSpace(name) == "" {
			return errors.New(`cannot find out the host name; set "host" in the config`)
		}
		c.Host = strings.TrimSpace(name)
	}
	if strings.ContainsAny(c.Host, "\r\n\t") {
		return errors.New(`"host" must be a single line`)
	}
	return nil
}

func (c *Config) validate() error {
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("log.level: %q is not one of debug, info, warn, error", c.Log.Level)
	}
	if c.Log.Format != "text" && c.Log.Format != "json" {
		return fmt.Errorf("log.format: %q is not one of text, json", c.Log.Format)
	}
	switch {
	case c.Run.Parallel < 1:
		return errors.New("run.parallel must be at least 1")
	case c.Defaults.Timeout <= 0:
		return errors.New("defaults.timeout must be positive")
	case c.Queue.TTL <= 0:
		return errors.New("queue.ttl must be positive")
	case c.Queue.FailedTTL <= 0:
		return errors.New("queue.failed_ttl must be positive")
	case c.Limits.Text < 1:
		return errors.New("limits.text must be positive")
	}
	if within(c.Paths.Spool, c.Paths.Failed) || within(c.Paths.Failed, c.Paths.Spool) {
		return errors.New("paths.spool and paths.failed must be different directories, neither inside the other")
	}
	for i, s := range c.Sources {
		if s.Type != SourceOnecheck {
			return fmt.Errorf("sources[%d].type: %q is not supported (known: %s)", i, s.Type, SourceOnecheck)
		}
		if s.Path == "" {
			return fmt.Errorf("sources[%d].path is required", i)
		}
	}
	return nil
}

// within reports whether path is dir or lies inside it.
func within(path, dir string) bool {
	rel, err := filepath.Rel(filepath.Clean(dir), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// resolvePaths makes relative directories relative to the config file.
func (c *Config) resolvePaths() {
	base := filepath.Dir(c.Path)
	paths := []*string{&c.Log.Dir, &c.Paths.Modules, &c.Paths.Spool, &c.Paths.Failed}
	for i := range c.Sources {
		paths = append(paths, &c.Sources[i].Path)
	}
	for _, p := range paths {
		if !filepath.IsAbs(*p) {
			*p = filepath.Join(base, *p)
		}
	}
}

func (c *Config) includeFiles() ([]string, error) {
	var files []string
	seen := map[string]bool{c.Path: true}
	for _, pattern := range c.Include {
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(filepath.Dir(c.Path), pattern)
		}
		matches, err := filepath.Glob(pattern)
		if err != nil {
			return nil, fmt.Errorf("include %q: %w", pattern, err)
		}
		sort.Strings(matches)
		for _, m := range matches {
			if info, err := os.Stat(m); err != nil || info.IsDir() || seen[m] {
				continue
			}
			seen[m] = true
			files = append(files, m)
		}
	}
	return files, nil
}

// addChannels converts channel nodes from one file and appends them.
func (c *Config) addChannels(nodes []yaml.Node, source string, sources map[string]string) error {
	for i := range nodes {
		node := &nodes[i]
		ch, err := c.buildChannel(node, source)
		if err != nil {
			return fmt.Errorf("%s: channel #%d (line %d): %w", source, i+1, node.Line, err)
		}
		if prev, dup := sources[ch.Name]; dup {
			return fmt.Errorf("duplicate channel name %q: defined in %s and in %s", ch.Name, prev, source)
		}
		sources[ch.Name] = source
		c.Channels = append(c.Channels, ch)
	}
	return nil
}

// moduleNameRE keeps a module name from pointing outside the modules directory.
var moduleNameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (c *Config) buildChannel(node *yaml.Node, source string) (Channel, error) {
	ch := Channel{Source: source, Enabled: true, Timeout: time.Duration(c.Defaults.Timeout)}
	var raw rawChannel
	// Decoding the node (not its text) keeps anchors defined elsewhere
	// usable; unknown keys are then checked by hand.
	if err := node.Decode(&raw); err != nil {
		return ch, errors.New(strings.TrimPrefix(err.Error(), "yaml: "))
	}
	if err := unknownChannelKeys(node); err != nil {
		return ch, err
	}
	ch.Name, ch.Module = raw.Name, raw.Module
	if raw.Enabled != nil {
		ch.Enabled = *raw.Enabled
	}
	if raw.Timeout != nil {
		ch.Timeout = time.Duration(*raw.Timeout)
	}
	switch {
	case ch.Name == "":
		return ch, errors.New(`"name" is required`)
	case !NameRE.MatchString(ch.Name):
		return ch, fmt.Errorf("invalid channel name %q: use lower-case letters, digits, '_', '.', '-' and start with a letter or digit", ch.Name)
	case ch.Module == "":
		return ch, fmt.Errorf(`channel %q: "module" is required`, ch.Name)
	case !moduleNameRE.MatchString(ch.Module):
		return ch, fmt.Errorf("channel %q: invalid module name %q: use lower-case letters, digits, '_', '-'", ch.Name, ch.Module)
	case ch.Timeout <= 0:
		return ch, fmt.Errorf("channel %q: timeout must be positive", ch.Name)
	}
	params, err := expandEnv(raw.Params)
	if err != nil {
		ch.Err = err
		return ch, nil
	}
	ch.Params, _ = params.(map[string]any)
	return ch, nil
}

var channelKeys = map[string]bool{
	"name": true, "module": true, "enabled": true, "timeout": true, "params": true,
	"<<": true, // YAML merge key
}

// unknownChannelKeys rejects keys a channel does not have, so typos are reported.
func unknownChannelKeys(node *yaml.Node) error {
	if node.Kind == yaml.AliasNode && node.Alias != nil {
		node = node.Alias
	}
	if node.Kind != yaml.MappingNode {
		return errors.New("a channel must be a mapping")
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if key := node.Content[i].Value; !channelKeys[key] {
			return fmt.Errorf("line %d: field %s not found in a channel (known: name, module, enabled, timeout, params)",
				node.Content[i].Line, key)
		}
	}
	return nil
}

// expandEnv replaces ${VAR} in every string of v with the environment value.
// "$${VAR}" produces a literal "${VAR}"; any other "$" is left alone. An
// undefined variable is an error.
func expandEnv(v any) (any, error) {
	switch x := v.(type) {
	case string:
		return expandString(x)
	case map[string]any:
		if x == nil {
			return x, nil
		}
		out := make(map[string]any, len(x))
		for k, item := range x {
			e, err := expandEnv(item)
			if err != nil {
				return nil, err
			}
			out[k] = e
		}
		return out, nil
	case []any:
		out := make([]any, len(x))
		for i, item := range x {
			e, err := expandEnv(item)
			if err != nil {
				return nil, err
			}
			out[i] = e
		}
		return out, nil
	}
	return v, nil
}

func expandString(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		rest := s[i:]
		switch {
		case strings.HasPrefix(rest, "$${"):
			// Escape: "$${VAR}" is a literal "${VAR}".
			b.WriteString("${")
			i += 2
		case strings.HasPrefix(rest, "${"):
			end := strings.IndexByte(rest, '}')
			if end < 0 {
				// The value is not quoted: params hold secrets.
				return "", errors.New("a param value has a \"${\" without the closing \"}\"; write \"$${\" for a literal \"${\"")
			}
			name := rest[2:end]
			val, ok := os.LookupEnv(name)
			if !ok {
				return "", fmt.Errorf("environment variable %s is not set (used as ${%s} in params)", name, name)
			}
			b.WriteString(val)
			i += end
		default:
			// Any other "$" is left alone.
			b.WriteByte(s[i])
		}
	}
	return b.String(), nil
}
