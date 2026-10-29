package cli

import (
	"context"
	"flag"
	"io"
	"os"
	"strings"

	"github.com/echoVic/curlgo/internal/core"
)

func readSource(path string, stdin io.Reader) ([]byte, error) {
	if path == "-" && stdin != nil {
		return readLimited(stdin, core.MaxInput)
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	return readLimited(file, core.MaxInput)
}

func configureEngine(e *core.Engine, settings core.Settings, options runOptions) *failure {
	ca := settings.CACertPEM
	if options.caFile != "" {
		data, err := readSource(options.caFile, nil)
		if err != nil {
			return inputFailure("INVALID_INPUT", "Invalid input.")
		}
		if ca != "" && strings.TrimSpace(ca) != strings.TrimSpace(string(data)) {
			return inputFailure("INVALID_INPUT", "Invalid input.")
		}
		ca = string(data)
	}
	if err := e.SetCACertificates(ca); err != nil {
		return inputFailure("INVALID_INPUT", "Invalid input.")
	}
	proxy := settings.Proxy
	if options.proxy != "" {
		commandURL, err := core.ParseProxy(options.proxy)
		if err != nil || commandURL == nil {
			return inputFailure("INVALID_INPUT", "Invalid input.")
		}
		if proxy.URL != "" {
			inputURL, err := core.ParseProxy(proxy.URL)
			if err != nil || inputURL == nil || commandURL.String() != inputURL.String() {
				return inputFailure("INVALID_INPUT", "Invalid input.")
			}
		}
		proxy.URL = commandURL.String()
	}
	if err := e.SetProxy(proxy); err != nil {
		return inputFailure("INVALID_INPUT", "Invalid input.")
	}
	return nil
}

func Run(ctx context.Context, stdin io.Reader, stdout, stderr io.Writer, args []string) ExitCode {
	var options runOptions
	flags := newRunFlagSet(&options)
	respondFailure := func(fail *failure) ExitCode {
		return emit(stdout, stderr, envelope{
			SchemaVersion: 1,
			Error:         &errorValue{Code: fail.Code, Message: fail.Message},
		}, fail.Exit)
	}
	parseErr := flags.Parse(args)
	if parseErr != nil || flags.NArg() != 0 {
		return respondFailure(inputFailure("INVALID_USAGE", "Invalid usage."))
	}
	if options.help {
		return emit(stdout, stderr, envelope{SchemaVersion: 1, OK: true, Help: runHelp(flags)}, ExitOK)
	}
	var requestSet, curlSet, outDirSet bool
	flags.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "request":
			requestSet = true
		case "curl":
			curlSet = true
		case "out-dir":
			outDirSet = true
		}
	})
	if requestSet == curlSet ||
		options.request == "" && options.curl == "" ||
		options.inlineBytes < 0 || options.inlineBytes > core.MaxBody {
		return respondFailure(inputFailure("INVALID_USAGE", "Invalid usage."))
	}
	path := options.request
	if path == "" {
		path = options.curl
	}
	data, err := readSource(path, stdin)
	if err != nil {
		return respondFailure(inputFailure("INVALID_INPUT", "Invalid input."))
	}
	input, fail := decodeInput(data, curlSet)
	if fail != nil {
		return respondFailure(fail)
	}
	engine := core.NewEngine()
	defer engine.Close()
	if fail := configureEngine(engine, input.Settings, options); fail != nil {
		return respondFailure(fail)
	}
	if options.dryRun {
		if fail := validateOutputPaths(options, outDirSet); fail != nil {
			return respondFailure(fail)
		}
	}
	var cookies []core.SavedCookie
	if options.cookieJar != "" {
		cookies, err = core.LoadCookieFile(options.cookieJar)
		if err != nil {
			return respondFailure(inputFailure("INVALID_INPUT", "Invalid input."))
		}
	}
	if options.dryRun {
		validation, fail := validateRequest(ctx, input.Request)
		if fail != nil {
			return respondFailure(fail)
		}
		return emit(stdout, stderr, envelope{SchemaVersion: 1, OK: true, Validation: validation}, ExitOK)
	}
	if options.cookieJar != "" {
		engine.RestoreCookies(cookies)
	}
	result, executionErr := engine.DoHTTP(ctx, input.Request, core.HTTPOptions{})
	fail = httpFailure(executionErr)
	var extracted map[string]string
	if executionErr == nil && result.StatusCode >= 200 && result.StatusCode < 300 {
		var extractionErr error
		extracted, extractionErr = core.ExtractVariables(input.Request.Extractors, result.Headers, result.Body, result.BodyTruncated)
		if extractionErr != nil {
			fail = &failure{Code: "EXTRACTION_ERROR", Message: "Response variable extraction failed.", Exit: ExitExecution}
		}
	}
	wireResult, bodyErr := formatResult(result, options.inlineBytes, options.outDir)
	if wireResult != nil {
		wireResult.ExtractedVariables = extracted
	}
	var cookieErr error
	if options.cookieJar != "" {
		cookieErr = core.SaveCookieFile(options.cookieJar, engine.CookieSnapshot())
	}
	if bodyErr != nil || cookieErr != nil {
		fail = outputFailure(fail)
	}
	value := envelope{SchemaVersion: 1, OK: fail == nil, Result: wireResult}
	exit := ExitOK
	if fail != nil {
		value.Error = &errorValue{Code: fail.Code, Message: fail.Message}
		exit = fail.Exit
	}
	return emit(stdout, stderr, value, exit)
}
