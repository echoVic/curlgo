package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/echoVic/curlgo/internal/core"
)

type helpTestValue struct {
	Usage   []string `json:"usage"`
	Options []struct {
		Name        string `json:"name"`
		ValueName   string `json:"valueName,omitempty"`
		Description string `json:"description"`
		Default     string `json:"default"`
	} `json:"options"`
	Protocols      []string `json:"protocols"`
	URLSchemes     []string `json:"urlSchemes"`
	Authentication []string `json:"authentication"`
	Limits         struct {
		MaxInputBytes   int `json:"maxInputBytes"`
		MaxBodyBytes    int `json:"maxBodyBytes"`
		InlineBodyBytes int `json:"inlineBodyBytes"`
	} `json:"limits"`
	ExitCodes []struct {
		Code    ExitCode `json:"code"`
		Meaning string   `json:"meaning"`
	} `json:"exitCodes"`
}

func TestRunHelpSkipsInputAndConfiguration(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing_private_marker")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{"long", []string{"--help"}},
		{"short", []string{"-h"}},
		{"single_dash_long", []string{"-help"}},
		{"double_dash_short", []string{"--h"}},
		{"stdin", []string{"--request", "-", "--help"}},
		{"curl_stdin", []string{"--curl", "-", "-h"}},
		{"missing_input", []string{"--request", missing, "--help"}},
		{"multiple_sources", []string{"--request", "-", "--curl", "-", "--help"}},
		{"empty_sources", []string{"--request=", "--curl=", "--help"}},
		{"negative_inline", []string{"--inline-body-bytes", "-1", "--help"}},
		{"oversized_inline", []string{"--inline-body-bytes", strconv.Itoa(core.MaxBody + 1), "--help"}},
		{"missing_config", []string{"--ca-file", missing, "--cookie-jar", missing, "--out-dir", missing, "--help"}},
		{"invalid_proxy", []string{"--proxy", "ftp://private_marker", "--help"}},
		{"dry_run", []string{"--dry-run", "--request", missing, "--out-dir=", "--help"}},
		{"alias_enables_long", []string{"--help=false", "-h"}},
		{"alias_enables_short", []string{"-h=false", "--help"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reader := &inputTestReader{
				Reader: strings.NewReader("private_marker"),
				err:    errors.New("stdin must not be read"),
			}
			exit, _, raw := runTestInvoke(t, reader, tc.args...)
			if exit != ExitOK || reader.read != 0 || strings.Contains(raw, "private_marker") {
				t.Fatalf("exit=%d, stdin read=%d, stdout=%q", exit, reader.read, raw)
			}
			helpTestDecode(t, raw)
		})
	}
}

func TestRunHelpRequiresValidFlagSyntax(t *testing.T) {
	for i, args := range [][]string{
		{"--help", "--private_marker"},
		{"--private_marker", "--help"},
		{"--help", "--dry-run=private_marker"},
		{"--help", "--request"},
		{"--help", "--ca-file"},
		{"--help", "--inline-body-bytes"},
		{"--help", "--inline-body-bytes=private_marker"},
		{"--inline-body-bytes=private_marker", "--help"},
		{"--help=private_marker"},
		{"-h=private_marker"},
		{"--help", "-h=private_marker"},
		{"--help", "private_marker"},
		{"private_marker", "--help"},
		{"--help", "--", "private_marker"},
		{"--help=true", "-h=false"},
		{"-h=true", "--help=false"},
	} {
		t.Run(strconv.Itoa(i), func(t *testing.T) {
			reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
			exit, value, raw := runTestInvoke(t, reader, args...)
			if exit != ExitInput || value.OK || value.Result != nil || value.Error == nil ||
				value.Error.Code != "INVALID_USAGE" || value.Error.Message == "" ||
				reader.read != 0 || strings.Contains(raw, "private_marker") {
				t.Fatalf("exit=%d, envelope=%+v, read=%d", exit, value, reader.read)
			}
			var wire map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &wire); err != nil {
				t.Fatal(err)
			}
			if _, exists := wire["help"]; exists {
				t.Fatal("usage failure must not include help")
			}
		})
	}
}

func TestHelpDescribesRunContract(t *testing.T) {
	exit, _, raw := runTestInvoke(t, nil, "--help")
	if exit != ExitOK {
		t.Fatalf("help exit=%d, stdout=%q", exit, raw)
	}
	value := helpTestDecode(t, raw)
	if len(value.Usage) == 0 || !strings.Contains(strings.Join(value.Usage, "\n"), "curlgo run") {
		t.Fatalf("missing run usage: %v", value.Usage)
	}
	if !reflect.DeepEqual(value.Protocols, []string{"HTTP"}) ||
		!reflect.DeepEqual(value.URLSchemes, []string{"http", "https"}) ||
		!reflect.DeepEqual(value.Authentication, []string{string(core.AuthBearer), string(core.AuthBasic), string(core.AuthAPIKey)}) {
		t.Fatalf("unsupported or missing capabilities: %+v", value)
	}
	if value.Limits.MaxInputBytes != core.MaxInput || value.Limits.MaxBodyBytes != core.MaxBody ||
		value.Limits.InlineBodyBytes != 256<<10 {
		t.Fatalf("help limits=%+v", value.Limits)
	}
	wantDefaults := map[string]string{
		"request": "", "curl": "", "ca-file": "", "cookie-jar": "", "proxy": "", "out-dir": "",
		"inline-body-bytes": strconv.Itoa(256 << 10), "help": "false", "h": "false", "dry-run": "false",
	}
	if len(value.Options) != len(wantDefaults) {
		t.Fatalf("options=%+v, want %d registered flags", value.Options, len(wantDefaults))
	}
	for i, option := range value.Options {
		want, exists := wantDefaults[option.Name]
		if !exists || option.Default != want || option.Description == "" ||
			i > 0 && value.Options[i-1].Name >= option.Name {
			t.Fatalf("unexpected, stale, or unsorted option metadata: %+v", option)
		}
		if option.Name == "help" || option.Name == "h" || option.Name == "dry-run" {
			if option.ValueName != "" {
				t.Fatalf("boolean help flag has value name: %+v", option)
			}
		} else if option.ValueName == "" {
			t.Fatalf("non-boolean flag lacks value name: %+v", option)
		}
		delete(wantDefaults, option.Name)
	}
	if len(wantDefaults) != 0 {
		t.Fatalf("missing registered options: %v", wantDefaults)
	}
	if len(value.ExitCodes) != 4 {
		t.Fatalf("exit codes=%+v", value.ExitCodes)
	}
	for i, code := range value.ExitCodes {
		if code.Code != ExitCode(i) || code.Meaning == "" {
			t.Fatalf("exit code metadata=%+v", code)
		}
	}
}

func TestHelpMetadataDoesNotUseRuntimeValues(t *testing.T) {
	_, _, baseline := runTestInvoke(t, nil, "--help")
	exit, _, changed := runTestInvoke(t, nil,
		"--request=private_marker", "--curl=private_marker", "--ca-file=private_marker",
		"--cookie-jar=private_marker", "--proxy=private_marker", "--out-dir=private_marker",
		"--inline-body-bytes=-1", "--help")
	if exit != ExitOK || changed != baseline || strings.Contains(changed, "private_marker") {
		t.Fatalf("help depends on runtime values: exit=%d, baseline=%q, changed=%q", exit, baseline, changed)
	}
	helpTestDecode(t, changed)
}

func TestRunHelpWriterFailure(t *testing.T) {
	for _, tc := range []struct {
		name    string
		partial int
		err     error
	}{
		{"error", 0, errors.New("private_marker")},
		{"partial_error", 12, errors.New("private_marker")},
		{"short_write", 12, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout := &outputTestFailingWriter{partial: tc.partial, err: tc.err}
			var stderr bytes.Buffer
			reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
			exit := Run(context.Background(), reader, stdout, &stderr, []string{"--help"})
			if exit != ExitOutput || reader.read != 0 || stdout.calls != 1 ||
				stdout.Len() != tc.partial || stderr.String() != "CurlGo: 无法写入结果 JSON\n" {
				t.Fatalf("exit=%d, stdin read=%d, writes=%d, stdout=%q, stderr=%q",
					exit, reader.read, stdout.calls, stdout.String(), stderr.String())
			}
		})
	}
}

func TestDispatchHelpForms(t *testing.T) {
	for _, args := range [][]string{
		{"help"}, {"help", "run"}, {"-h"}, {"--help"},
		{"run", "-h"}, {"run", "--help", "--request", "-", "--curl", "-"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
			var stdout, stderr bytes.Buffer
			exit, handled := Dispatch(context.Background(), reader, &stdout, &stderr, args)
			if !handled || exit != ExitOK || reader.read != 0 || stderr.Len() != 0 {
				t.Fatalf("handled=%v, exit=%d, read=%d, stderr=%q", handled, exit, reader.read, stderr.String())
			}
			value := helpTestDecode(t, stdout.String())
			usage := strings.Join(value.Usage, "\n")
			isRoot := args[0] != "run" && len(args) == 1
			if strings.Contains(usage, "curlgo help [run]") != isRoot {
				t.Fatalf("incorrect help topic for %v: %v", args, value.Usage)
			}
		})
	}
}

func TestDispatchRejectsInvalidRootHelp(t *testing.T) {
	for _, args := range [][]string{
		{"help", "private_marker"}, {"help", "run", "private_marker"},
		{"help", "run", "--help"}, {"help", "--help"},
		{"-h", "run"}, {"--help", "private_marker"}, {"--help", "--help"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
			var stdout, stderr bytes.Buffer
			exit, handled := Dispatch(context.Background(), reader, &stdout, &stderr, args)
			var value envelope
			if err := decodeOne(stdout.Bytes(), &value); err != nil {
				t.Fatal(err)
			}
			if !handled || exit != ExitInput || value.OK || value.Result != nil || value.Help != nil ||
				value.Error == nil || value.Error.Code != "INVALID_USAGE" || value.Error.Message != "Invalid help command. Use 'curlgo help' or 'curlgo help run'." ||
				reader.read != 0 || stderr.Len() != 0 || strings.Contains(stdout.String(), "private_marker") {
				t.Fatalf("handled=%v, exit=%d, envelope=%+v, read=%d, stderr=%q",
					handled, exit, value, reader.read, stderr.String())
			}
		})
	}
}

func TestDispatchLeavesGUIArgumentsUnhandled(t *testing.T) {
	for _, args := range [][]string{
		nil, {}, {"-render", "private_marker"}, {"--render", "private_marker", "-dark"},
		{"render"}, {"-dark"}, {"-psn_0_12345"}, {"native_gui_argument"}, {"dry-run"},
	} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
			var stdout, stderr bytes.Buffer
			exit, handled := Dispatch(context.Background(), reader, &stdout, &stderr, args)
			if handled || exit != ExitOK || reader.read != 0 || stdout.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("handled=%v, exit=%d, read=%d, stdout=%q, stderr=%q",
					handled, exit, reader.read, stdout.String(), stderr.String())
			}
		})
	}
}

func TestDispatchRunPreservesContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var stdout, stderr bytes.Buffer
	exit, handled := Dispatch(ctx,
		strings.NewReader(`{"schemaVersion":1,"request":{"url":"http://example.invalid"}}`),
		&stdout, &stderr, []string{"run", "--request", "-"})
	var value envelope
	if err := decodeOne(stdout.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if !handled || exit != ExitExecution || value.OK || value.Result != nil || value.Help != nil ||
		value.Error == nil || value.Error.Code != "CANCELED" || stderr.Len() != 0 {
		t.Fatalf("handled=%v, exit=%d, envelope=%+v, stderr=%q", handled, exit, value, stderr.String())
	}
}

func TestDispatchHelpWriterFailure(t *testing.T) {
	for _, args := range [][]string{{"help"}, {"help", "run"}, {"--help"}, {"run", "-h"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			stdout := &outputTestFailingWriter{err: errors.New("private_marker")}
			var stderr bytes.Buffer
			exit, handled := Dispatch(context.Background(), nil, stdout, &stderr, args)
			if !handled || exit != ExitOutput || stdout.calls != 1 || stdout.Len() != 0 ||
				stderr.String() != "CurlGo: 无法写入结果 JSON\n" {
				t.Fatalf("handled=%v, exit=%d, writes=%d, stdout=%q, stderr=%q",
					handled, exit, stdout.calls, stdout.String(), stderr.String())
			}
		})
	}
}

func TestHelpOptionsMatchRegisteredFlags(t *testing.T) {
	var options runOptions
	flags := newRunFlagSet(&options)
	if err := flags.Parse([]string{"--help", "--request=private_marker", "--inline-body-bytes=-1"}); err != nil {
		t.Fatal(err)
	}
	_, _, raw := runTestInvoke(t, nil, "--help")
	value := helpTestDecode(t, raw)
	i := 0
	flags.VisitAll(func(f *flag.Flag) {
		if i >= len(value.Options) {
			t.Fatalf("registered flag %s is not documented", f.Name)
		}
		option := value.Options[i]
		name, usage := flag.UnquoteUsage(f)
		if option.Name != f.Name || option.ValueName != name ||
			option.Description != usage || option.Default != f.DefValue {
			t.Fatalf("help option=%+v, actual flag=%+v", option, f)
		}
		i++
	})
	if i != len(value.Options) {
		t.Fatalf("documented %d options but registered %d", len(value.Options), i)
	}
}

func helpTestDecode(t *testing.T, raw string) helpTestValue {
	t.Helper()
	var wire map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &wire); err != nil {
		t.Fatal(err)
	}
	outputTestKeys(t, outputTestObject(t, wire), "schemaVersion", "ok", "help")
	var value struct {
		SchemaVersion int           `json:"schemaVersion"`
		OK            bool          `json:"ok"`
		Help          helpTestValue `json:"help"`
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		t.Fatalf("invalid help JSON: %v, stdout=%q", err, raw)
	}
	if err := decoder.Decode(new(any)); err != io.EOF || value.SchemaVersion != 1 || !value.OK {
		t.Fatalf("not one successful schema-1 help document: %v, stdout=%q", err, raw)
	}
	return value.Help
}
