package cli

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
)

func TestSkillGuideAndTopics(t *testing.T) {
	for topic, heading := range map[string]string{
		"": "# API Test", "api-test": "# API Test",
		"cli-reference": "# CurlGo CLI Reference",
		"workflows":     "# CurlGo Request Workflows", "assertions": "# 声明式响应断言",
		"response-extraction": "# 响应变量提取", "project-secrets": "# 机密变量",
	} {
		t.Run(topic, func(t *testing.T) {
			args := []string{"skill"}
			if topic != "" {
				args = append(args, topic)
			}
			reader := &inputTestReader{Reader: strings.NewReader("private_marker"), err: errors.New("must not read stdin")}
			var out, errOut bytes.Buffer
			exit, handled := Dispatch(context.Background(), reader, &out, &errOut, args)
			if !handled || exit != ExitOK || reader.read != 0 || errOut.Len() != 0 {
				t.Fatalf("handled=%v exit=%d reads=%d stderr=%q", handled, exit, reader.read, errOut.String())
			}
			text := out.String()
			if !strings.Contains(text, "\n"+heading+"\n") {
				t.Fatalf("wrong guide for %q: %q", topic, text)
			}
			if !strings.Contains(text, "CurlGo CLI ") || !strings.HasSuffix(text, "[curlgo:skill] end of guide\n") || len(text) < 500 {
				t.Fatalf("incomplete guide: %q", text)
			}
			if strings.Contains(text, "](references/") || strings.Contains(text, "](workflows.md)") {
				t.Fatal("guide depends on files outside the installed binary")
			}
		})
	}
}

func TestSkillRejectsInvalidTopicsAndFlags(t *testing.T) {
	for _, args := range [][]string{{"skill", "private_marker"}, {"skill", "../guide"}, {"skill", "/etc/passwd"}, {"skill", "workflows", "private_marker"}, {"skill", "--request", "-"}} {
		var out, errOut bytes.Buffer
		reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
		exit, handled := Dispatch(context.Background(), reader, &out, &errOut, args)
		if !handled || exit != ExitInput || reader.read != 0 || errOut.Len() != 0 {
			t.Fatalf("args=%v handled=%v exit=%d reads=%d", args, handled, exit, reader.read)
		}
		var value envelope
		if err := decodeOne(out.Bytes(), &value); err != nil {
			t.Fatal(err)
		}
		if value.Error == nil || value.Error.Code != "INVALID_USAGE" || strings.Contains(out.String(), "private_marker") {
			t.Fatalf("unsafe usage output: %q", out.String())
		}
	}
}

func TestSkillOutputFailure(t *testing.T) {
	for _, writeErr := range []error{nil, errors.New("private_marker")} {
		out := &outputTestFailingWriter{partial: 12, err: writeErr}
		var errOut bytes.Buffer
		exit, handled := Dispatch(context.Background(), nil, out, &errOut, []string{"skill"})
		if !handled || exit != ExitOutput || out.calls != 1 || errOut.Len() == 0 || strings.Contains(errOut.String(), "private_marker") {
			t.Fatalf("handled=%v exit=%d writes=%d stderr=%q", handled, exit, out.calls, errOut.String())
		}
	}
}

func TestSkillCanBeDiscoveredWithoutProbingUnknownCommands(t *testing.T) {
	reader := &inputTestReader{Reader: strings.NewReader("private_marker")}
	var out, errOut bytes.Buffer
	exit, handled := Dispatch(context.Background(), reader, &out, &errOut, []string{"help"})
	if !handled || exit != ExitOK || reader.read != 0 || errOut.Len() != 0 {
		t.Fatalf("help discovery handled=%v exit=%d read=%d stderr=%q", handled, exit, reader.read, errOut.String())
	}
	value := helpTestDecode(t, out.String())
	for _, usage := range value.Usage {
		if usage == "curlgo skill [topic]" {
			return
		}
	}
	t.Fatalf("bootstrap cannot safely discover skill support: %v", value.Usage)
}
