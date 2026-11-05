package cli

import (
	"context"
	"flag"
	"io"

	"github.com/echoVic/curlgo/internal/core"
)

type helpValue struct {
	Usage          []string       `json:"usage"`
	Options        []helpOption   `json:"options"`
	Protocols      []string       `json:"protocols"`
	URLSchemes     []string       `json:"urlSchemes"`
	Authentication []string       `json:"authentication"`
	Limits         helpLimits     `json:"limits"`
	ExitCodes      []helpExitCode `json:"exitCodes"`
}

type helpOption struct {
	Name        string `json:"name"`
	ValueName   string `json:"valueName,omitempty"`
	Description string `json:"description"`
	Default     string `json:"default"`
}

type helpLimits struct {
	MaxInputBytes   int `json:"maxInputBytes"`
	MaxBodyBytes    int `json:"maxBodyBytes"`
	InlineBodyBytes int `json:"inlineBodyBytes"`
}

type helpExitCode struct {
	Code    ExitCode `json:"code"`
	Meaning string   `json:"meaning"`
}

func runHelp(flags *flag.FlagSet) *helpValue {
	value := &helpValue{
		Usage: []string{
			"curlgo run (--request path | --curl path) [options]",
			"curlgo run -h | --help",
		},
		Protocols:      []string{"HTTP"},
		URLSchemes:     []string{"http", "https"},
		Authentication: []string{string(core.AuthBearer), string(core.AuthBasic), string(core.AuthAPIKey)},
		Limits: helpLimits{
			MaxInputBytes: core.MaxInput, MaxBodyBytes: core.MaxBody,
			InlineBodyBytes: defaultInlineBodyBytes,
		},
		ExitCodes: []helpExitCode{
			{ExitOK, "Success; HTTP responses, including 4xx and 5xx, are not execution failures."},
			{ExitInput, "Invalid usage, input, or request, including MISSING_SECRET and INVALID_SECRETS."},
			{ExitExecution, "HTTP execution failure, including timeout or cancellation."},
			{ExitOutput, "Unable to write output."},
			{ExitAssertion, "Response assertions failed; the captured HTTP response is retained."},
		},
	}
	flags.VisitAll(func(f *flag.Flag) {
		name, usage := flag.UnquoteUsage(f)
		value.Options = append(value.Options, helpOption{
			Name: f.Name, ValueName: name, Description: usage, Default: f.DefValue,
		})
	})
	return value
}

// Dispatch handles CLI commands without consulting the GUI workspace.
func Dispatch(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) (ExitCode, bool) {
	if len(args) == 0 {
		return ExitOK, false
	}
	var root bool
	switch args[0] {
	case "skill":
		return skill(stdout, stderr, args[1:]), true
	case "run":
		return Run(ctx, stdin, stdout, stderr, args[1:]), true
	case "help":
		root = len(args) == 1
		if !root && (len(args) != 2 || args[1] != "run") {
			return emit(stdout, stderr, envelope{
				SchemaVersion: 1,
				Error:         &errorValue{Code: "INVALID_USAGE", Message: "Invalid help command. Use 'curlgo help' or 'curlgo help run'."},
			}, ExitInput), true
		}
	case "-h", "--help":
		root = true
		if len(args) != 1 {
			return emit(stdout, stderr, envelope{
				SchemaVersion: 1,
				Error:         &errorValue{Code: "INVALID_USAGE", Message: "Invalid help command. Use 'curlgo help' or 'curlgo help run'."},
			}, ExitInput), true
		}
	default:
		return ExitOK, false
	}
	var options runOptions
	value := runHelp(newRunFlagSet(&options))
	if root {
		value.Usage = []string{
			"curlgo",
			"curlgo run (--request path | --curl path) [options]",
			"curlgo help [run]",
			"curlgo skill [topic]",
			"curlgo -h | --help",
		}
	}
	return emit(stdout, stderr, envelope{SchemaVersion: 1, OK: true, Help: value}, ExitOK), true
}
