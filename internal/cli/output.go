package cli

import (
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/echoVic/curlgo/internal/core"
)

type envelope struct {
	SchemaVersion int          `json:"schemaVersion"`
	OK            bool         `json:"ok"`
	Result        *resultValue `json:"result,omitempty"`
	Error         *errorValue  `json:"error,omitempty"`
}

type resultValue struct {
	Method     string      `json:"method"`
	URL        string      `json:"url"`
	Status     string      `json:"status"`
	Proto      string      `json:"proto"`
	StatusCode int         `json:"statusCode"`
	Headers    http.Header `json:"headers"`
	Body       *bodyValue  `json:"body,omitempty"`
	Timing     timingValue `json:"timing"`
}

type errorValue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type bodyValue struct {
	Mode        string  `json:"mode"`
	Encoding    string  `json:"encoding"`
	ContentType string  `json:"contentType"`
	Bytes       int     `json:"bytes"`
	Text        *string `json:"text,omitempty"`
	Path        string  `json:"path,omitempty"`
	Truncated   bool    `json:"truncated"`
}

type timingValue struct {
	DNSMs     float64 `json:"dnsMs"`
	ConnectMs float64 `json:"connectMs"`
	TLSMs     float64 `json:"tlsMs"`
	TTFBMs    float64 `json:"ttfbMs"`
	TotalMs   float64 `json:"totalMs"`
}

func httpFailure(err error) *failure {
	if err == nil {
		return nil
	}
	var typed *core.HTTPError
	if errors.As(err, &typed) && typed != nil {
		switch typed.Code {
		case core.HTTPErrInvalidRequest:
			return inputFailure(string(typed.Code), "Invalid request.")
		case core.HTTPErrRequestBuild:
			return inputFailure(string(typed.Code), "Unable to build HTTP request.")
		case core.HTTPErrRedirect:
			return &failure{Code: string(typed.Code), Message: "HTTP redirect failed.", Exit: ExitExecution}
		case core.HTTPErrTLS:
			return &failure{Code: string(typed.Code), Message: "TLS request failed.", Exit: ExitExecution}
		case core.HTTPErrConnect:
			return &failure{Code: string(typed.Code), Message: "HTTP connection failed.", Exit: ExitExecution}
		case core.HTTPErrTimeout:
			return &failure{Code: string(typed.Code), Message: "HTTP request timed out.", Exit: ExitExecution}
		case core.HTTPErrCanceled:
			return &failure{Code: string(typed.Code), Message: "HTTP request canceled.", Exit: ExitExecution}
		case core.HTTPErrBodyRead:
			return &failure{Code: string(typed.Code), Message: "Unable to read HTTP response body.", Exit: ExitExecution}
		}
	}
	return &failure{Code: string(core.HTTPErrConnect), Message: "HTTP connection failed.", Exit: ExitExecution}
}

func outputFailure(primary *failure) *failure {
	message := "Unable to write output."
	if primary != nil && primary.Code != "" {
		message = "Unable to write output (primary error: " + primary.Code + ")."
	}
	return &failure{Code: "OUTPUT_ERROR", Message: message, Exit: ExitOutput}
}

func formatResult(input core.HTTPResult, limit int, dir string) (*resultValue, error) {
	if input.StatusCode == 0 {
		return nil, nil
	}
	result := &resultValue{
		Method: input.Method, URL: input.URL, Status: input.Status, Proto: input.Proto,
		StatusCode: input.StatusCode, Headers: input.Headers.Clone(),
		Timing: timingValue{
			DNSMs:     float64(input.Timing.DNS) / float64(time.Millisecond),
			ConnectMs: float64(input.Timing.Connect) / float64(time.Millisecond),
			TLSMs:     float64(input.Timing.TLS) / float64(time.Millisecond),
			TTFBMs:    float64(input.Timing.TTFB) / float64(time.Millisecond),
			TotalMs:   float64(input.Timing.Total) / float64(time.Millisecond),
		},
	}
	contentType := input.Headers.Get("Content-Type")
	if contentType == "" {
		contentType = http.DetectContentType(input.Body)
	}
	body := &bodyValue{
		ContentType: contentType, Bytes: len(input.Body), Truncated: input.BodyTruncated,
	}
	if len(input.Body) == 0 || len(input.Body) <= limit && isInlineText(input.Body, contentType) {
		text := string(input.Body)
		body.Mode, body.Encoding, body.Text = "inline", "utf-8", &text
	} else {
		path, err := saveBody(input.Body, dir)
		if err != nil {
			return result, err
		}
		body.Mode, body.Encoding, body.Path = "file", "binary", path
	}
	result.Body = body
	return result, nil
}

func isInlineText(body []byte, contentType string) bool {
	if !utf8.Valid(body) {
		return false
	}
	for _, r := range string(body) {
		if unicode.IsControl(r) && r != '\t' && r != '\r' && r != '\n' {
			return false
		}
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	switch mediaType {
	case "application/json", "application/xml", "application/javascript", "application/x-www-form-urlencoded":
		return true
	}
	return strings.HasPrefix(mediaType, "text/") ||
		strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}

func saveBody(body []byte, dir string) (path string, err error) {
	if dir == "" {
		dir, err = os.MkdirTemp("", "curlgo-response-*")
		if err != nil {
			return "", err
		}
		tempDir := dir
		defer func() {
			if err != nil {
				err = errors.Join(err, os.RemoveAll(tempDir))
			}
		}()
		if err = os.Chmod(dir, 0700); err != nil {
			return "", err
		}
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(dir)
	if err != nil {
		return "", err
	}
	if !info.IsDir() {
		return "", errors.New("output path is not a directory")
	}
	file, err := os.CreateTemp(dir, "response-*.body")
	if err != nil {
		return "", err
	}
	savedPath := file.Name()
	closed := false
	defer func() {
		if !closed {
			err = errors.Join(err, file.Close())
		}
		if err != nil {
			removeErr := os.Remove(savedPath)
			if !errors.Is(removeErr, os.ErrNotExist) {
				err = errors.Join(err, removeErr)
			}
			path = ""
		}
	}()
	if err = file.Chmod(0600); err != nil {
		return "", err
	}
	n, err := file.Write(body)
	if err != nil {
		return "", err
	}
	if n != len(body) {
		return "", io.ErrShortWrite
	}
	if err = file.Sync(); err != nil {
		return "", err
	}
	err = file.Close()
	closed = true
	if err != nil {
		return "", err
	}
	return savedPath, nil
}

func emit(stdout, stderr io.Writer, value envelope, exit ExitCode) ExitCode {
	data, err := json.Marshal(value)
	if err == nil {
		data = append(data, '\n')
		var n int
		n, err = stdout.Write(data)
		if err == nil && n != len(data) {
			err = io.ErrShortWrite
		}
	}
	if err != nil {
		_, _ = stderr.Write([]byte("CurlGo: 无法写入结果 JSON\n"))
		return ExitOutput
	}
	return exit
}
