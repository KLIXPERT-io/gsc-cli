package client

import (
	"context"
	"errors"
	"net/http"
	"strings"

	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
	"google.golang.org/api/googleapi"
	"google.golang.org/api/option"
	pagespeedonline "google.golang.org/api/pagespeedonline/v5"
)

// PSI wraps the PageSpeed Insights API v5.
type PSI struct {
	Svc *pagespeedonline.Service
}

// NewPSI builds a PSI client using the shared OAuth *http.Client.
func NewPSI(ctx context.Context, httpClient *http.Client) (*PSI, error) {
	svc, err := pagespeedonline.NewService(ctx, option.WithHTTPClient(httpClient))
	if err != nil {
		return nil, err
	}
	return &PSI{Svc: svc}, nil
}

// TranslatePSI converts a PSI googleapi error to a structured errs.E.
func TranslatePSI(err error) error {
	if err == nil {
		return nil
	}
	var ge *googleapi.Error
	if errors.As(err, &ge) {
		msg := strings.ToLower(ge.Message)
		switch {
		case ge.Code == 401:
			return errs.New(errs.CodeAuthExpired, ge.Message).WithHint("Run `gsc auth login`.")
		case ge.Code == 403 && (strings.Contains(msg, "service_disabled") || strings.Contains(msg, "permission_denied") || strings.Contains(msg, "has not been used") || strings.Contains(msg, "disabled")):
			return errs.New(errs.CodeAuthRequired, ge.Message).WithHint("Enable the PageSpeed Insights API for the GCP project backing your credentials: https://console.cloud.google.com/apis/library/pagespeedonline.googleapis.com")
		case ge.Code == 403 && strings.Contains(msg, "quota"):
			return errs.New(errs.CodeQuotaExceeded, ge.Message).WithRetry(3600)
		case ge.Code == 403:
			return errs.New(errs.CodeAuthDenied, ge.Message)
		case ge.Code == 404:
			return errs.New(errs.CodeNotFound, ge.Message)
		case ge.Code == 429:
			return errs.New(errs.CodeRateLimited, ge.Message).WithRetry(60).WithHint("Run `gsc quota` to inspect the `psi` bucket usage.")
		case ge.Code >= 500:
			return errs.New(errs.CodeAPI5xx, ge.Message).WithRetry(30)
		case ge.Code >= 400:
			return errs.New(errs.CodeInvalidArgs, ge.Message)
		}
	}
	m := err.Error()
	if strings.Contains(m, "no such host") || strings.Contains(m, "dial tcp") {
		return errs.New(errs.CodeNetworkUnreachable, m)
	}
	return errs.New(errs.CodeGeneric, m)
}
