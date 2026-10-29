package core

import (
	"context"
	"errors"
	"testing"
)

func TestHeadlessErrorTypes(t *testing.T) {
	var err error = &HTTPError{Code: HTTPErrTimeout, Op: "do", Err: context.DeadlineExceeded}
	var got *HTTPError
	if !errors.As(err, &got) || got.Code != HTTPErrTimeout ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("typed error lost: %v", err)
	}
	_, err = Expand("{{token}}", nil)
	var missing *UnresolvedVariableError
	if !errors.As(err, &missing) || missing.Name != "token" ||
		err.Error() != "环境变量 token 未定义" {
		t.Fatalf("variable error lost: %v", err)
	}
}
