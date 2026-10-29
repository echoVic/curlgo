package core

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/http/httptrace"
	"net/url"
	"strings"
	"time"
)

type httpRedirectTransport struct {
	*http.Transport
	followRedirects bool
	response        *http.Response
	redirectErr     *HTTPError
}

func (t *httpRedirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.Transport.RoundTrip(req)
	if err != nil {
		return resp, err
	}
	switch resp.StatusCode {
	case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
		http.StatusTemporaryRedirect, http.StatusPermanentRedirect:
		if _, err := resp.Location(); err != nil && !errors.Is(err, http.ErrNoLocation) {
			resp.Request = req
			t.response = resp
			if t.followRedirects {
				// Client.Do formats this parse error without preserving its cause.
				t.redirectErr = &HTTPError{Code: HTTPErrRedirect, Op: "redirect", Err: err}
			} else {
				// Client.Do parses Location before CheckRedirect, even when following is disabled.
				response := *resp
				response.Header = resp.Header.Clone()
				response.Header.Del("Location")
				return &response, nil
			}
		}
	}
	return resp, nil
}

func (e *Engine) DoHTTP(parent context.Context, r Request, options HTTPOptions) (result HTTPResult, err error) {
	state, trace := newHTTPTrace(time.Now(), time.Now)
	defer func() { result.Timing = state.finish() }()

	if r.Protocol != "HTTP" {
		return result, &HTTPError{Code: HTTPErrInvalidRequest, Op: "validate", Err: fmt.Errorf("unsupported protocol %q", r.Protocol)}
	}
	u, err := url.Parse(r.URL)
	if err != nil {
		return result, &HTTPError{Code: HTTPErrInvalidRequest, Op: "validate", Err: err}
	}
	if (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" {
		return result, &HTTPError{Code: HTTPErrInvalidRequest, Op: "validate", Err: fmt.Errorf("HTTP URL requires an http or https scheme and a host")}
	}
	if r.TimeoutSeconds < 1 || r.TimeoutSeconds > 3600 {
		return result, &HTTPError{Code: HTTPErrInvalidRequest, Op: "validate", Err: fmt.Errorf("timeout must be between 1 and 3600 seconds")}
	}

	ctx, cancel := context.WithTimeout(parent, time.Duration(r.TimeoutSeconds)*time.Second)
	defer func() {
		state.finish()
		cancel()
	}()
	ctx = httptrace.WithClientTrace(ctx, trace)
	if err := ctx.Err(); err != nil {
		return result, classifyHTTPError(ctx, "build", HTTPErrRequestBuild, err)
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, r.URL, strings.NewReader(r.Body))
	if err != nil {
		return result, classifyHTTPError(ctx, "build", HTTPErrRequestBuild, err)
	}
	req.Header = HTTPHeaders(r.Headers)
	switch r.BodyMode {
	case BodyRaw:
		if r.Body != "" && req.Header.Get("Content-Type") == "" {
			req.Header.Set("Content-Type", "application/octet-stream")
		}
	case BodyMultipart:
		boundary := multipart.NewWriter(io.Discard).Boundary()
		body, size, err := openMultipart(r.FormParts, boundary)
		if err != nil {
			return result, classifyHTTPError(ctx, "build", HTTPErrRequestBuild, err)
		}
		defer func() {
			state.finish()
			body.Close()
		}()
		req.Body, req.ContentLength = body, size
		req.Header.Set("Content-Type", "multipart/form-data; boundary="+boundary)
		if r.FollowRedirects {
			req.GetBody = func() (io.ReadCloser, error) {
				if err := ctx.Err(); err != nil {
					return nil, &HTTPError{Code: HTTPErrRedirect, Op: "redirect", Err: err}
				}
				body, length, err := openMultipart(r.FormParts, boundary)
				if err != nil {
					return nil, &HTTPError{Code: HTTPErrRedirect, Op: "redirect", Err: err}
				}
				if err := ctx.Err(); err != nil {
					body.Close()
					return nil, &HTTPError{Code: HTTPErrRedirect, Op: "redirect", Err: err}
				}
				if length != size {
					body.Close()
					return nil, &HTTPError{Code: HTTPErrRedirect, Op: "redirect", Err: fmt.Errorf("multipart upload size changed before redirect")}
				}
				return body, nil
			}
		}
	default:
		return result, &HTTPError{Code: HTTPErrInvalidRequest, Op: "build", Err: fmt.Errorf("unsupported body mode %q", r.BodyMode)}
	}
	if host := req.Header.Get("Host"); host != "" {
		req.Host = host
		req.Header.Del("Host")
	}

	client := e.client(r)
	transport := &httpRedirectTransport{
		Transport:       client.Transport.(*http.Transport),
		followRedirects: r.FollowRedirects,
	}
	transport.MaxResponseHeaderBytes = 256 << 10
	client.Transport = transport
	defer func() {
		state.finish()
		client.CloseIdleConnections()
	}()
	checkRedirect := client.CheckRedirect
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		err := checkRedirect(req, via)
		if err == nil || errors.Is(err, http.ErrUseLastResponse) {
			return err
		}
		return &HTTPError{Code: HTTPErrRedirect, Op: "redirect", Err: err}
	}
	resp, err := client.Do(req)
	if transport.response != nil {
		resp = transport.response
	}
	if resp != nil {
		result = responseHTTPResult(resp)
		defer func() {
			state.finish()
			resp.Body.Close()
		}()
	}
	if err != nil {
		if transport.redirectErr != nil {
			err = errors.Join(err, transport.redirectErr)
		}
		return result, classifyHTTPError(ctx, "do", HTTPErrConnect, proxyError(err))
	}

	limit := options.MaxBodyBytes
	if limit <= 0 || limit > MaxBody {
		limit = MaxBody
	}
	result.Body, err = io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if len(result.Body) > limit {
		result.Body = result.Body[:limit]
		result.BodyTruncated = true
		return result, nil
	}
	if err != nil {
		return result, classifyHTTPError(ctx, "read", HTTPErrBodyRead, err)
	}
	return result, nil
}

func classifyHTTPError(ctx context.Context, op string, fallback HTTPErrorCode, err error) error {
	if err == nil {
		return nil
	}
	code := fallback
	var timeout net.Error
	var typed *HTTPError
	var verification *tls.CertificateVerificationError
	var record tls.RecordHeaderError
	var unknown x509.UnknownAuthorityError
	var hostname x509.HostnameError
	var invalid x509.CertificateInvalidError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded) || errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &timeout) && timeout.Timeout():
		code = HTTPErrTimeout
	case errors.Is(ctx.Err(), context.Canceled) || errors.Is(err, context.Canceled):
		code = HTTPErrCanceled
	case errors.As(err, &typed):
		code = typed.Code
	case errors.As(err, &verification) || errors.As(err, &record) || errors.As(err, &unknown) ||
		errors.As(err, &hostname) || errors.As(err, &invalid):
		code = HTTPErrTLS
	}
	return &HTTPError{Code: code, Op: op, Err: err}
}

func responseHTTPResult(resp *http.Response) HTTPResult {
	result := HTTPResult{
		Proto:      resp.Proto,
		StatusCode: resp.StatusCode,
		Status:     resp.Status,
		Headers:    resp.Header.Clone(),
	}
	if resp.Request != nil {
		result.Method = resp.Request.Method
		if resp.Request.URL != nil {
			result.URL = resp.Request.URL.String()
		}
	}
	return result
}
