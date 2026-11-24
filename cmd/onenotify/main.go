// Command onenotify delivers notifications about onecheck events and
// messages of your own.
package main

import (
	"os"

	"github.com/alekzonder/onenotify/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}
