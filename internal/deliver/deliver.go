package deliver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/alekzonder/onenotify/internal/config"
	"github.com/alekzonder/onenotify/internal/modules"
	"github.com/alekzonder/onenotify/internal/queue"
	"github.com/alekzonder/onenotify/internal/schema"
	"github.com/alekzonder/onenotify/pkg/module"
)

// Options narrow a run and make it testable.
type Options struct {
	// Channels limits delivery to these channels; empty means all. Events
	// are still taken in for every enabled channel.
	Channels []string
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Log receives the run's log records; nil discards them.
	Log *slog.Logger
}

func (o Options) selected(channel string) bool {
	if len(o.Channels) == 0 {
		return true
	}
	for _, c := range o.Channels {
		if c == channel {
			return true
		}
	}
	return false
}

// Run takes events in and delivers the queues. The caller holds the run lock.
//
// A returned error means onenotify itself could not do its job (its own files
// could not be written); the report is still valid for what was done before.
// A channel that cannot deliver is not an error: its messages wait.
func Run(ctx context.Context, cfg *config.Config, q *queue.Queue, opt Options) (*Report, error) {
	if opt.Log == nil {
		opt.Log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	now := time.Now
	if opt.Now != nil {
		now = opt.Now
	}
	r := &run{ctx: ctx, cfg: cfg, q: q, opt: opt, log: opt.Log, now: now, mods: map[string]*loaded{}}
	report := &Report{Records: []Record{}}

	taken, err := intake(cfg, q, now().UTC(), r.log)
	report.Ingested = taken
	if err != nil {
		// Do not deliver: see intake.
		return report, err
	}

	names, err := r.channelNames()
	if err != nil {
		return report, err
	}
	results := make([]channelResult, len(names))
	sem := make(chan struct{}, cfg.Run.Parallel)
	var wg sync.WaitGroup
	// Describe the modules before the channels start: after this the map
	// is only read.
	for _, name := range names {
		r.preload(name)
	}
	for i, name := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = r.serve(name)
		}()
	}
	wg.Wait()

	var errs []error
	for _, res := range results {
		report.Records = append(report.Records, res.records...)
		report.Waiting += res.waiting
		if res.err != nil {
			errs = append(errs, res.err)
		}
	}
	for _, rec := range report.Records {
		switch rec.Outcome {
		case Sent:
			report.Sent++
		case Failed:
			report.Failed++
		}
	}
	r.log.Info("run finished", "ingested", report.Ingested, "sent", report.Sent,
		"waiting", report.Waiting, "failed", report.Failed)
	if ctx.Err() != nil {
		errs = append(errs, errors.New("run interrupted"))
	}
	return report, errors.Join(errs...)
}

type run struct {
	ctx  context.Context
	cfg  *config.Config
	q    *queue.Queue
	opt  Options
	log  *slog.Logger
	now  func() time.Time
	mods map[string]*loaded // by module name; filled before the channels start
}

// loaded is a module ready to be used, or the reason it cannot be.
type loaded struct {
	info   *modules.Info
	schema *schema.Schema
	err    error
}

type channelResult struct {
	records []Record
	waiting int
	err     error
}

// channelNames is every channel that may have messages: those in the config
// and those that only have a queue directory left.
func (r *run) channelNames() ([]string, error) {
	onDisk, err := r.q.Channels()
	if err != nil {
		return nil, fmt.Errorf("cannot read the spool: %w", err)
	}
	seen := map[string]bool{}
	var names []string
	for _, name := range onDisk {
		seen[name] = true
		names = append(names, name)
	}
	for _, ch := range r.cfg.Channels {
		if !seen[ch.Name] {
			names = append(names, ch.Name)
		}
	}
	sort.Strings(names)
	return names, nil
}

// preload describes the module of a channel that will be served, once per
// module, before any channel starts.
func (r *run) preload(name string) {
	ch := r.cfg.Channel(name)
	if ch == nil || !ch.Enabled || !r.opt.selected(name) {
		return
	}
	if _, done := r.mods[ch.Module]; done {
		return
	}
	m := &loaded{}
	m.info, m.err = modules.Load(r.ctx, r.cfg.Paths.Modules, ch.Module)
	if m.err == nil {
		r.log.Debug("module loaded", "module", m.info.Name, "version", m.info.Version,
			"protocol", m.info.Protocol, "path", m.info.Path)
		m.schema, m.err = schema.Compile(m.info.ParamsSchema)
	}
	r.mods[ch.Module] = m
}

// serve goes through one channel's queue in order.
func (r *run) serve(name string) channelResult {
	var res channelResult
	files, err := r.q.List(name)
	if err != nil {
		res.err = fmt.Errorf("cannot read the queue of channel %s: %w", name, err)
		return res
	}
	ch := r.cfg.Channel(name)
	served := ch != nil && ch.Enabled && r.opt.selected(name)
	if len(files) == 0 {
		if ch == nil {
			// What a removed channel left behind is of no use any more.
			r.q.Forget(name)
		}
		return res
	}
	defer func() {
		// A queue that ended up empty has nothing to be failing about.
		if left, err := r.q.List(name); err == nil && len(left) == 0 {
			_ = r.q.ClearStatus(name)
		}
	}()

	var sender *sender
	stopped := !served
	// unusable is why the channel cannot be used at all; it is reported
	// with the first message that has to wait because of it.
	unusable := ""
	if served {
		if sender, err = r.sender(ch); err != nil {
			stopped, unusable = true, err.Error()
		} else if st := r.q.Status(name); st != nil && r.mustWait(st) {
			stopped = true
			r.log.Info("channel asked to wait, not sending yet", "channel", name, "retry_at", st.RetryAt.UTC())
		}
	}

	for _, file := range files {
		if r.ctx.Err() != nil {
			stopped = true
		}
		msg, err := r.q.Load(name, file)
		if err != nil {
			stopped = !r.fail(&res, name, moduleOf(ch), file, strings.TrimSuffix(file, ".json"), "unreadable queue file: "+err.Error()) || stopped
			continue
		}
		if !r.now().Before(msg.ExpiresAt) {
			stopped = !r.fail(&res, name, moduleOf(ch), file, msg.ID, "expired at "+msg.ExpiresAt.UTC().Format(time.RFC3339)) || stopped
			continue
		}
		if stopped {
			if served {
				res.waiting++
			}
			if unusable != "" {
				res.records = append(res.records, Record{Channel: name, Module: ch.Module, ID: msg.ID,
					Outcome: Waiting, Error: unusable})
				r.channelFailed(name, unusable, 0)
				unusable = ""
			}
			continue
		}
		rec := sender.send(r, msg.Wire())
		switch rec.Outcome {
		case Sent:
			if err := r.q.Remove(name, file); err != nil {
				res.err = fmt.Errorf("message %s was sent to %s but cannot be removed from the queue (it would be sent again): %w",
					msg.ID, name, err)
				stopped = true
			}
			r.channelWorks(name)
		case Failed:
			// A message that cannot be put away must not be overtaken.
			stopped = !r.fail(&res, name, ch.Module, file, msg.ID, rec.Error)
			continue
		case Waiting:
			stopped = true
			res.waiting++
		}
		res.records = append(res.records, rec)
	}
	return res
}

func moduleOf(ch *config.Channel) string {
	if ch == nil {
		return ""
	}
	return ch.Module
}

// maxRetryAfter bounds how long a module can keep a channel closed. Without
// it one wrong answer, or a clock that was ahead for a moment, would close a
// channel until someone deletes its status file.
const maxRetryAfter = 24 * time.Hour

// mustWait reports whether a failing channel asked not to be used yet.
func (r *run) mustWait(st *queue.Status) bool {
	if st.RetryAt == nil {
		return false
	}
	left := st.RetryAt.Sub(r.now())
	return left > 0 && left <= maxRetryAfter
}

// fail moves a message to the failed store and reports it. It returns false
// when the message could not be moved: it is then still in the queue, and
// the channel must stop so that nothing overtakes it.
func (r *run) fail(res *channelResult, channel, mod, file, id, reason string) bool {
	if err := r.q.Fail(channel, file, reason, r.now()); err != nil {
		res.err = fmt.Errorf("cannot move message %s of channel %s to the failed store: %w", id, channel, err)
		return false
	}
	r.log.Warn("message will not be delivered; moved to the failed store",
		"channel", channel, "id", id, "reason", reason)
	res.records = append(res.records, Record{Channel: channel, Module: mod, ID: id, Outcome: Failed, Error: reason})
	return true
}

// channelFailed remembers that a channel could not deliver. The first failure
// is a warning (which reaches stderr, so cron mails it); repeats are logged
// quietly until the channel works again.
func (r *run) channelFailed(channel, reason string, retryAfter time.Duration) {
	if r.ctx.Err() != nil {
		return // an interrupted run says nothing about the channel
	}
	now := r.now().UTC()
	st := r.q.Status(channel)
	if st == nil {
		st = &queue.Status{FailingSince: now}
		r.log.Warn("channel cannot deliver; messages wait and will be retried", "channel", channel, "error", reason)
	} else {
		r.log.Info("channel still cannot deliver", "channel", channel, "error", reason,
			"failing_since", st.FailingSince.UTC(), "runs", st.Consecutive+1)
	}
	st.Consecutive++
	st.LastError = reason
	st.RetryAt = nil
	if retryAfter > 0 {
		at := now.Add(min(retryAfter, maxRetryAfter))
		st.RetryAt = &at
	}
	if err := r.q.SetStatus(channel, *st); err != nil {
		r.log.Warn("cannot save the channel status", "channel", channel, "error", err)
	}
}

// channelWorks forgets a channel's failures after a delivery.
func (r *run) channelWorks(channel string) {
	st := r.q.Status(channel)
	if st == nil {
		return
	}
	r.log.Info("channel delivers again", "channel", channel, "was_failing_since", st.FailingSince.UTC())
	if err := r.q.ClearStatus(channel); err != nil {
		r.log.Warn("cannot clear the channel status", "channel", channel, "error", err)
	}
}

// sender delivers messages of one channel through its module.
type sender struct {
	ch     *config.Channel
	info   *modules.Info
	params json.RawMessage
}

// sender checks that a channel can be used: its config, its module and its
// params. The error never quotes a param value.
func (r *run) sender(ch *config.Channel) (*sender, error) {
	if ch.Err != nil {
		return nil, ch.Err
	}
	m := r.mods[ch.Module]
	if m == nil {
		return nil, fmt.Errorf("module %s was not loaded", ch.Module)
	}
	if m.err != nil {
		return nil, m.err
	}
	params := ch.Params
	if params == nil {
		params = map[string]any{}
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, errors.New("invalid params: they cannot be encoded as JSON")
	}
	if err := m.schema.Validate(raw); err != nil {
		return nil, err
	}
	return &sender{ch: ch, info: m.info, params: raw}, nil
}

func (s *sender) send(r *run, msg module.Message) Record {
	rec := Record{Channel: s.ch.Name, Module: s.ch.Module, ID: msg.ID}
	stderr := &tail{max: 16 << 10}
	start := time.Now()
	// The request is not logged: its params hold the channel's secrets.
	r.log.Debug("sending", "channel", s.ch.Name, "module", s.ch.Module, "id", msg.ID,
		"params", paramNames(s.ch.Params), "timeout", s.ch.Timeout)
	resp, err := modules.Send(r.ctx, s.info, module.Request{Channel: s.ch.Name, Params: s.params, Message: msg},
		s.ch.Timeout, stderr)
	rec.DurationMS = time.Since(start).Milliseconds()
	if len(stderr.buf) > 0 {
		r.log.Debug("module stderr", "channel", s.ch.Name, "module", s.ch.Module, "output", strings.TrimSpace(string(stderr.buf)))
	}
	switch {
	case err != nil:
		rec.Outcome, rec.Error = Waiting, err.Error()
		r.channelFailed(s.ch.Name, rec.Error, 0)
	case resp.Delivered:
		rec.Outcome = Sent
		r.log.Info("message sent", "channel", s.ch.Name, "module", s.ch.Module, "id", msg.ID, "duration_ms", rec.DurationMS)
	case resp.Error.Permanent:
		rec.Outcome, rec.Error = Failed, resp.Error.Message
	default:
		rec.Outcome, rec.Error = Waiting, resp.Error.Message
		r.channelFailed(s.ch.Name, rec.Error, time.Duration(resp.Error.RetryAfterMS)*time.Millisecond)
	}
	return rec
}

// tail keeps the first max bytes written to it and drops the rest, so that a
// module cannot fill the memory through its stderr.
type tail struct {
	buf []byte
	max int
}

func (t *tail) Write(p []byte) (int, error) {
	if room := t.max - len(t.buf); room > 0 {
		t.buf = append(t.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

// paramNames is what may be logged about a channel's params: their names.
func paramNames(params map[string]any) []string {
	names := make([]string, 0, len(params))
	for name := range params {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// CheckChannel reports why a channel cannot be used as configured, without
// sending anything: its config, its module, its params. The error never
// quotes a param value.
func CheckChannel(ctx context.Context, cfg *config.Config, ch *config.Channel) error {
	if ch.Err != nil {
		return ch.Err
	}
	r := &run{ctx: ctx, cfg: cfg, log: slog.New(slog.NewTextHandler(io.Discard, nil)), mods: map[string]*loaded{}}
	m := &loaded{}
	m.info, m.err = modules.Load(ctx, cfg.Paths.Modules, ch.Module)
	if m.err == nil {
		m.schema, m.err = schema.Compile(m.info.ParamsSchema)
	}
	r.mods[ch.Module] = m
	_, err := r.sender(ch)
	return err
}
