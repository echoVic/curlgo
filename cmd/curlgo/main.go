// Command curlgo is the standalone Agent CLI. It does not initialize the GUI.
package main

import (
	"os"

	"github.com/echoVic/curlgo/internal/cli"
)

func main() {
	os.Exit(int(cli.Command(os.Stdin, os.Stdout, os.Stderr, os.Args[1:])))
}
