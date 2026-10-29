package core

import (
	"fmt"
	"net/http"
	"time"
)

type HTTPOptions struct{ MaxBodyBytes int }

type HTTPTiming struct {
	DNS, Connect, TLS, TTFB, Total time.Duration
}

type HTTPResult struct {
	Method, URL, Proto, Status string
	StatusCode                 int
	Headers                    http.Header
	Body                       []byte
	BodyTruncated              bool
	Timing                     HTTPTiming
}

type HTTPErrorCode string

const (
	HTTPErrInvalidRequest HTTPErrorCode = "INVALID_REQUEST"
	HTTPErrRequestBuild   HTTPErrorCode = "REQUEST_BUILD_ERROR"
	HTTPErrRedirect       HTTPErrorCode = "REDIRECT_ERROR"
	HTTPErrTLS            HTTPErrorCode = "TLS_ERROR"
	HTTPErrConnect        HTTPErrorCode = "CONNECT_ERROR"
	HTTPErrTimeout        HTTPErrorCode = "TIMEOUT"
	HTTPErrCanceled       HTTPErrorCode = "CANCELED"
	HTTPErrBodyRead       HTTPErrorCode = "BODY_READ_ERROR"
)

type HTTPError struct {
	Code HTTPErrorCode
	Op   string
	Err  error
}

func (e *HTTPError) Error() string {
	return fmt.Sprintf("%s: %s: %v", e.Code, e.Op, e.Err)
}
func (e *HTTPError) Unwrap() error { return e.Err }

type UnresolvedVariableError struct{ Name string }

func (e *UnresolvedVariableError) Error() string {
	return fmt.Sprintf("环境变量 %s 未定义", e.Name)
}
