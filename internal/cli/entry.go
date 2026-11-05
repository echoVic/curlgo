package cli

import (
	"context"
	"io"
	"os"
	"os/signal"
)

// Command runs the standalone CLI. Unlike the desktop entry point, an empty
// command shows help, and unknown commands never fall through to GUI startup.
func Command(stdin io.Reader, stdout, stderr io.Writer, args []string) ExitCode {
	ctx := context.Background()
	if len(args) > 0 && args[0] == "run" {
		var stop context.CancelFunc
		ctx, stop = signal.NotifyContext(ctx, os.Interrupt)
		defer stop()
	}
	if len(args) == 0 {
		args = []string{"help"}
	}
	exit, handled := Dispatch(ctx, NewInterruptibleInput(ctx, stdin), stdout, stderr, args)
	if handled {
		return exit
	}
	return emit(stdout, stderr, envelope{
		SchemaVersion: 1,
		Error:         &errorValue{Code: "INVALID_USAGE", Message: "Unknown command. Use 'curlgo help' for usage."},
	}, ExitInput)
}
