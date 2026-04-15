// Package client is a thin wrapper around the Google Search Console API.
package client

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	searchconsole "google.golang.org/api/searchconsole/v1"
)

type Client struct {
	Svc   *searchconsole.Service
	Calls int
}

func New(ctx context.Context, httpClient *http.Client) (*Client, error) {
	svc, err := searchconsole.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &Client{Svc: svc}, nil
}

// Translate converts an API error into a structured errs.E.
func Translate(err error) error {
	if err == nil {
		return nil
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		switch {
		case ge.Code == 401:
			return errs.New(errs.CodeAuthExpired, ge.Message).WithHint("Run `gsc auth login`.")
		case ge.Code == 403 && strings.Contains(strings.ToLower(ge.Message), "quota"):
			return errs.New(errs.CodeQuotaExceeded, ge.Message).WithRetry(3600)
		case ge.Code == 403:
			return errs.New(errs.CodeAuthDenied, ge.Message)
		case ge.Code == 404:
			return errs.New(errs.CodePropertyNotFound, ge.Message)
		case ge.Code == 429:
			return errs.New(errs.CodeRateLimited, ge.Message).WithRetry(60)
		case ge.Code >= 500:
			return errs.New(errs.CodeAPI5xx, ge.Message).WithRetry(30)
		case ge.Code >= 400:
			return errs.New(errs.CodeInvalidArgs, ge.Message)
		}
	}
	msg := err.Error()
	if strings.Contains(msg, "no such host") || strings.Contains(msg, "dial tcp") {
		return errs.New(errs.CodeNetworkUnreachable, msg)
	}
	return errs.New(errs.CodeGeneric, msg)
}
