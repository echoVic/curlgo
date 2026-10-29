package cli

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/echoVic/curlgo/internal/core"
)

type validationValue struct {
	DryRun          bool          `json:"dryRun"`
	Method          string        `json:"method"`
	Protocol        string        `json:"protocol"`
	BodyMode        core.BodyMode `json:"bodyMode"`
	HeaderCount     int           `json:"headerCount"`
	FormPartCount   int           `json:"formPartCount"`
	TimeoutSeconds  int           `json:"timeoutSeconds"`
	FollowRedirects bool          `json:"followRedirects"`
}

func validateOutputPaths(options runOptions, outDirSet bool) *failure {
	isDirectory := func(path string) bool {
		info, err := os.Stat(path)
		return err == nil && info.IsDir()
	}
	if outDirSet && !isDirectory(options.outDir) ||
		options.cookieJar != "" && !isDirectory(filepath.Dir(options.cookieJar)) {
		return outputFailure(nil)
	}
	return nil
}

func validationContextFailure(ctx context.Context) *failure {
	err := ctx.Err()
	if err == nil {
		return nil
	}
	code := core.HTTPErrCanceled
	if errors.Is(err, context.DeadlineExceeded) {
		code = core.HTTPErrTimeout
	}
	return httpFailure(&core.HTTPError{Code: code, Op: "validate", Err: err})
}

func validateRequest(ctx context.Context, request core.Request) (*validationValue, *failure) {
	if fail := validationContextFailure(ctx); fail != nil {
		return nil, fail
	}
	var err error
	if request.BodyMode == core.BodyMultipart {
		err = core.ValidateMultipartFiles(request.FormParts)
	}
	if fail := validationContextFailure(ctx); fail != nil {
		return nil, fail
	}
	if err != nil {
		return nil, httpFailure(&core.HTTPError{Code: core.HTTPErrRequestBuild, Op: "build", Err: err})
	}
	return &validationValue{
		DryRun: true, Method: request.Method, Protocol: request.Protocol,
		BodyMode: request.BodyMode, HeaderCount: len(request.Headers), FormPartCount: len(request.FormParts),
		TimeoutSeconds: request.TimeoutSeconds, FollowRedirects: request.FollowRedirects,
	}, nil
}
