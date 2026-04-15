// Package errs defines structured errors and exit codes per PRD FR-8.
package errs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

type Code string

const (
	CodeAuthMissing     Code = "auth_missing"
	CodeAuthExpired     Code = "auth_expired"
	CodeAuthDenied      Code = "auth_denied"
	CodeQuotaExceeded   Code = "quota_exceeded"
	CodeRateLimited     Code = "rate_limited"
	CodePropertyNotFound Code = "property_not_found"
	CodeSitemapNotFound Code = "sitemap_not_found"
	CodeURLNotIndexed   Code = "url_not_indexed"
	CodeInvalidArgs     Code = "invalid_args"
	CodeInvalidDateRange Code = "invalid_date_range"
	CodeNetworkUnreachable Code = "network_unreachable"
	CodeAPI5xx          Code = "api_5xx"
	CodeGeneric         Code = "generic"
)

type E struct {
	Code          Code   `json:"code"`
	Message       string `json:"message"`
	Hint          string `json:"hint,omitempty"`
	Retriable     bool   `json:"retriable"`
	RetryAfterSec int    `json:"retry_after_sec,omitempty"`
}

func (e *E) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

func New(code Code, msg string) *E       { return &E{Code: code, Message: msg} }
func Newf(code Code, f string, a ...any) *E { return &E{Code: code, Message: fmt.Sprintf(f, a...)} }

func (e *E) WithHint(h string) *E           { e.Hint = h; return e }
func (e *E) WithRetry(after int) *E         { e.Retriable = true; e.RetryAfterSec = after; return e }

// ExitCode maps a Code to the CLI exit code per FR-8.
func ExitCode(err error) int {
	if err == nil {
		return 0
	}
	var e *E
	if !errors.As(err, &e) {
		return 1
	}
	switch e.Code {
	case CodeAuthMissing, CodeAuthExpired, CodeAuthDenied:
		return 2
	case CodeQuotaExceeded, CodeRateLimited:
		return 3
	case CodePropertyNotFound, CodeSitemapNotFound, CodeURLNotIndexed:
		return 4
	case CodeInvalidArgs, CodeInvalidDateRange:
		return 5
	case CodeNetworkUnreachable, CodeAPI5xx:
		return 6
	default:
		return 1
	}
}

// Write renders an error as a single JSON line to stderr per FR-8.
func Write(w io.Writer, err error) {
	if err == nil {
		return
	}
	var e *E
	if !errors.As(err, &e) {
		e = &E{Code: CodeGeneric, Message: err.Error()}
	}
	payload := struct {
		Error *E `json:"error"`
	}{Error: e}
	b, _ := json.Marshal(payload)
	fmt.Fprintln(w, string(b))
}
