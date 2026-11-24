// Command fakemod is a module for tests. Its behaviour is chosen by the
// channel's params (for "send") and by FAKEMOD_MODE (for "describe").
//
// params:
//
//	mode            ok (default), temporary, permanent, retry_after, exit3,
//	                garbage, silent, hang, ok_then_exit3, ok_then_hang, huge
//	only            apply mode only to messages whose id contains this text;
//	                other messages are delivered
//	record          file to append every received request to, one JSON per line
//	retry_after_ms  for mode retry_after
//	pidfile         for mode hang: where to write the pid of a child process
//	token           a secret, to check that it is never logged
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/alekzonder/onenotify/pkg/module"
)

type params struct {
	Mode         string `json:"mode"`
	Only         string `json:"only"`
	Record       string `json:"record"`
	RetryAfterMS int64  `json:"retry_after_ms"`
	PidFile      string `json:"pidfile"`
	Token        string `json:"token"`
}

const schema = `{
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "mode": {"type": "string"},
    "only": {"type": "string"},
    "record": {"type": "string"},
    "retry_after_ms": {"type": "integer"},
    "pidfile": {"type": "string"},
    "token": {"type": "string"}
  }
}`

func main() {
	if len(os.Args) > 1 && os.Args[1] == "describe" {
		describe(os.Getenv("FAKEMOD_MODE"))
	}
	module.Main(module.Module{
		Description: module.Description{
			Name:         "fake",
			Version:      "1.2.3",
			Description:  "fake module for tests",
			ParamsSchema: json.RawMessage(schema),
		},
		Send: send,
	})
}

// describe misbehaves on request; otherwise it returns and the SDK answers.
func describe(mode string) {
	switch mode {
	case "describe-garbage":
		fmt.Println("this is not json")
		os.Exit(0)
	case "describe-fail":
		os.Exit(3)
	case "describe-hang":
		// The child inherits stdout and keeps the pipe open.
		child := exec.Command("sleep", "20")
		child.Stdout = os.Stdout
		if err := child.Start(); err != nil {
			panic(err)
		}
		time.Sleep(20 * time.Second)
	case "protocol99":
		fmt.Println(`{"protocol":99,"name":"fake","version":"9.0.0"}`)
		os.Exit(0)
	case "other-name":
		fmt.Println(`{"protocol":1,"name":"other","version":"1.0.0"}`)
		os.Exit(0)
	}
}

func send(_ context.Context, req module.Request) error {
	var p params
	if err := module.DecodeParams(req.Params, &p); err != nil {
		return err
	}
	fmt.Fprintln(os.Stderr, "fakemod: sending", req.Message.ID, "to", req.Channel)
	if p.Record != "" {
		line, _ := json.Marshal(req)
		f, err := os.OpenFile(p.Record, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			return err
		}
		_, _ = f.Write(append(line, '\n'))
		f.Close()
	}
	if p.Only != "" && !strings.Contains(req.Message.ID, p.Only) {
		return nil
	}
	return misbehave(p)
}

func misbehave(p params) error {
	switch p.Mode {
	case "temporary":
		return errors.New("server is down")
	case "permanent":
		return module.Permanent(errors.New("message rejected"))
	case "retry_after":
		return module.RetryAfter(errors.New("too many requests"), time.Duration(p.RetryAfterMS)*time.Millisecond)
	case "exit3":
		fmt.Fprintln(os.Stderr, "boom on stderr")
		os.Exit(3)
	case "garbage":
		fmt.Println("{not json")
		os.Exit(0)
	case "silent":
		os.Exit(0)
	case "ok_then_exit3":
		fmt.Println(`{"delivered":true,"error":null}`)
		os.Exit(3)
	case "ok_then_hang":
		fmt.Println(`{"delivered":true,"error":null}`)
		time.Sleep(60 * time.Second)
	case "no_reason":
		fmt.Println(`{"delivered":false,"error":null}`)
		os.Exit(0)
	case "hang":
		child := exec.Command("sleep", "60")
		if err := child.Start(); err != nil {
			panic(err)
		}
		if p.PidFile != "" {
			_ = os.WriteFile(p.PidFile, []byte(strconv.Itoa(child.Process.Pid)), 0o644)
		}
		time.Sleep(60 * time.Second)
	case "huge":
		buf := make([]byte, 1<<20)
		for i := range buf {
			buf[i] = 'a'
		}
		for i := 0; i < 8; i++ {
			_, _ = os.Stdout.Write(buf)
		}
		os.Exit(0)
	}
	return nil
}
