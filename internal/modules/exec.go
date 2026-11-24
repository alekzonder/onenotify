package modules

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"syscall"
	"time"

	"github.com/alekzonder/onenotify/pkg/module"
)

// killGrace is how long a module may overrun its timeout to report the
// timeout itself before onenotify kills it.
const killGrace = 500 * time.Millisecond

// describeLimit caps the size of a module description.
const describeLimit = 1 << 20

var errTooLarge = errors.New("output too large")

// limitedBuffer keeps at most max bytes and remembers an overflow.
type limitedBuffer struct {
	buf      bytes.Buffer // not embedded: its ReadFrom would bypass the limit
	max      int64
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if int64(b.buf.Len()+len(p)) > b.max {
		b.overflow = true
		return 0, errTooLarge
	}
	return b.buf.Write(p)
}

// outcome says how a module process ended.
type outcome int

const (
	finished    outcome = iota // the process exited by itself; err holds its exit status
	timedOut                   // killed because it exceeded its time limit
	interrupted                // killed because onenotify itself is stopping
)

// run starts one module process and waits for it. The process gets its own
// process group, and on timeout or interruption the whole group is killed,
// so that nothing the module started survives or keeps a pipe open.
func run(ctx context.Context, path, arg string, stdin []byte, stdout, stderr io.Writer, limit time.Duration) (outcome, error) {
	runCtx, cancel := context.WithTimeout(ctx, limit)
	defer cancel()

	cmd := exec.CommandContext(runCtx, path, arg)
	cmd.Stdin = bytes.NewReader(stdin)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = time.Second

	err := cmd.Run()
	switch {
	case ctx.Err() != nil:
		return interrupted, ctx.Err()
	case runCtx.Err() != nil:
		return timedOut, runCtx.Err()
	}
	return finished, err
}

// ErrInterrupted means onenotify was told to stop while a module was running.
var ErrInterrupted = errors.New("interrupted")

// sendLimit caps the size of a module's answer to "send".
const sendLimit = 1 << 20

// Send asks the module to deliver one message and returns its answer. An
// error means the module itself misbehaved: timeout, crash, invalid output.
// Such an error says nothing about the message, so the caller keeps the
// message and tries again later.
func Send(ctx context.Context, info *Info, req module.Request, timeout time.Duration, stderr io.Writer) (*module.Response, error) {
	req.Protocol = info.Protocol
	req.TimeoutMS = timeout.Milliseconds()
	input, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("cannot encode request: %w", err)
	}
	stdout := &limitedBuffer{max: sendLimit}
	how, err := run(ctx, info.Path, "send", input, stdout, stderr, timeout+killGrace)
	switch {
	case how == interrupted:
		return nil, fmt.Errorf("module %q: %w", info.Name, ErrInterrupted)
	case how == timedOut:
		return nil, fmt.Errorf("module %q timed out after %s", info.Name, timeout)
	case stdout.overflow:
		return nil, fmt.Errorf("module %q: output too large (limit %d bytes)", info.Name, stdout.max)
	case err != nil:
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("module %q failed with exit code %d", info.Name, exitErr.ExitCode())
		}
		return nil, fmt.Errorf("module %q could not be started: %w", info.Name, err)
	}
	var resp module.Response
	if err := json.Unmarshal(stdout.buf.Bytes(), &resp); err != nil {
		return nil, fmt.Errorf("module %q returned invalid JSON: %w", info.Name, err)
	}
	if !resp.Delivered && (resp.Error == nil || resp.Error.Message == "") {
		return nil, fmt.Errorf("module %q did not deliver and gave no reason", info.Name)
	}
	return &resp, nil
}

// Describe runs "<path> describe" and returns the module's description.
func Describe(ctx context.Context, path string) (*Info, error) {
	stdout := &limitedBuffer{max: describeLimit}
	how, err := run(ctx, path, "describe", nil, stdout, io.Discard, describeTimeout)
	switch {
	case how == interrupted:
		return nil, fmt.Errorf("%s describe: %w", path, ErrInterrupted)
	case how == timedOut:
		return nil, fmt.Errorf("%s describe: no answer within %s", path, describeTimeout)
	case stdout.overflow:
		return nil, fmt.Errorf("%s describe: output too large", path)
	case err != nil:
		return nil, fmt.Errorf("%s describe: %w", path, err)
	}
	info := &Info{Path: path}
	if err := json.Unmarshal(stdout.buf.Bytes(), &info.Description); err != nil {
		return nil, fmt.Errorf("%s describe: invalid JSON: %w", path, err)
	}
	if info.Name == "" || info.Protocol == 0 {
		return nil, fmt.Errorf("%s describe: name and protocol are required", path)
	}
	return info, nil
}
