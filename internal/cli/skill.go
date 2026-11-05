package cli

import (
	"embed"
	"fmt"
	"io"
	"strings"
)

// runtimeVersion versions the CLI and its embedded guide together. Desktop
// packaging has a separate version in mygo.json.
//
//go:embed VERSION
var runtimeVersion string

//go:embed guide/*.md
var skillGuides embed.FS

func skill(stdout, stderr io.Writer, args []string) ExitCode {
	topic := "api-test"
	if len(args) == 1 {
		topic = args[0]
	}
	switch topic {
	case "api-test", "cli-reference", "workflows", "assertions", "response-extraction", "project-secrets":
	default:
		return invalidSkillUsage(stdout, stderr)
	}
	if len(args) > 1 {
		return invalidSkillUsage(stdout, stderr)
	}
	// topic is allowlisted above; files are compile-time resources, never user paths.
	data, err := skillGuides.ReadFile("guide/" + topic + ".md")
	if err != nil {
		fmt.Fprintln(stderr, "CurlGo: embedded skill guide is unavailable")
		return ExitOutput
	}
	text := fmt.Sprintf("# CurlGo CLI %s — %s\n\n", strings.TrimSpace(runtimeVersion), topic) + string(data)
	if topic == "api-test" {
		text += "\n## Bundled References\n\nRead only the topics needed for this task:\n\n"
		for _, name := range []string{"cli-reference", "workflows", "assertions", "response-extraction", "project-secrets"} {
			text += "- `curlgo skill " + name + "`\n"
		}
	}
	text += "\n[curlgo:skill] end of guide\n"
	n, err := stdout.Write([]byte(text))
	if err != nil || n != len(text) {
		fmt.Fprintln(stderr, "CurlGo: cannot write complete skill guide")
		return ExitOutput
	}
	return ExitOK
}

func invalidSkillUsage(stdout, stderr io.Writer) ExitCode {
	return emit(stdout, stderr, envelope{
		SchemaVersion: 1,
		Error:         &errorValue{Code: "INVALID_USAGE", Message: "Use 'curlgo skill' or 'curlgo skill <topic>': api-test, cli-reference, workflows, assertions, response-extraction, project-secrets."},
	}, ExitInput)
}
