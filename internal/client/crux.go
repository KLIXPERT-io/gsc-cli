package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
)

const (
	cruxQueryURL   = "https://chromeuxreport.googleapis.com/v1/records:queryRecord"
	cruxHistoryURL = "https://chromeuxreport.googleapis.com/v1/records:queryHistoryRecord"
)

// CrUX is a hand-rolled client against the Chrome UX Report API.
// The API requires an API key (OAuth is not supported); APIKey is appended
// as ?key=<value> on every request.
type CrUX struct {
	HTTP   *http.Client
	APIKey string
}

// NewCrUX returns a CrUX client. The httpClient is only used for transport
// (timeouts, retries); CrUX rejects OAuth bearer tokens, so an unauthenticated
// http.DefaultClient is fine.
func NewCrUX(httpClient *http.Client, apiKey string) *CrUX {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}
	return &CrUX{HTTP: httpClient, APIKey: apiKey}
}

// CruxRequest is the request body for queryRecord / queryHistoryRecord.
type CruxRequest struct {
	URL                   string   `json:"url,omitempty"`
	Origin                string   `json:"origin,omitempty"`
	FormFactor            string   `json:"formFactor,omitempty"` // PHONE | DESKTOP | TABLET; omit => ALL
	Metrics               []string `json:"metrics,omitempty"`
	CollectionPeriodCount int      `json:"collectionPeriodCount,omitempty"`
}

// CruxPercentiles carries the p75 value (API returns values as strings sometimes).
type CruxPercentiles struct {
	P75 json.RawMessage `json:"p75,omitempty"`
}

// CruxHistogramBin is one histogram bucket.
type CruxHistogramBin struct {
	Start   json.RawMessage `json:"start,omitempty"`
	End     json.RawMessage `json:"end,omitempty"`
	Density float64         `json:"density,omitempty"`
}

// CruxMetric is the per-metric shape returned by queryRecord.
type CruxMetric struct {
	Percentiles CruxPercentiles    `json:"percentiles,omitempty"`
	Histogram   []CruxHistogramBin `json:"histogram,omitempty"`
}

// CruxTimeseriesPercentiles for history responses.
type CruxTimeseriesPercentiles struct {
	P75s []json.RawMessage `json:"p75s,omitempty"`
}

// CruxTimeseriesHistogramBin for history responses.
type CruxTimeseriesHistogramBin struct {
	Start     json.RawMessage `json:"start,omitempty"`
	End       json.RawMessage `json:"end,omitempty"`
	Densities []float64       `json:"densities,omitempty"`
}

// CruxTimeseriesMetric is per-metric in history responses.
type CruxTimeseriesMetric struct {
	Percentiles          CruxTimeseriesPercentiles    `json:"percentilesTimeseries,omitempty"`
	HistogramTimeseries  []CruxTimeseriesHistogramBin `json:"histogramTimeseries,omitempty"`
}

// CruxCollectionPeriod is an ISO date range.
type CruxCollectionPeriod struct {
	FirstDate CruxDate `json:"firstDate"`
	LastDate  CruxDate `json:"lastDate"`
}

type CruxDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

// CruxKey identifies the returned record.
type CruxKey struct {
	URL        string `json:"url,omitempty"`
	Origin     string `json:"origin,omitempty"`
	FormFactor string `json:"formFactor,omitempty"`
}

// CruxRecord is the current record response.
type CruxRecord struct {
	Key               CruxKey               `json:"key"`
	Metrics           map[string]CruxMetric `json:"metrics"`
	CollectionPeriod  CruxCollectionPeriod  `json:"collectionPeriod"`
}

// CruxQueryResponse is the top-level queryRecord response.
type CruxQueryResponse struct {
	Record     CruxRecord `json:"record"`
	URLNormalizationDetails map[string]any `json:"urlNormalizationDetails,omitempty"`
}

// CruxHistoryRecord is the history record response.
type CruxHistoryRecord struct {
	Key                CruxKey                          `json:"key"`
	Metrics            map[string]CruxTimeseriesMetric  `json:"metrics"`
	CollectionPeriods  []CruxCollectionPeriod           `json:"collectionPeriods"`
}

// CruxHistoryResponse is the top-level queryHistoryRecord response.
type CruxHistoryResponse struct {
	Record                  CruxHistoryRecord `json:"record"`
	URLNormalizationDetails map[string]any    `json:"urlNormalizationDetails,omitempty"`
}

type cruxErrorEnvelope struct {
	Err struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error"`
}

// QueryRecord calls :queryRecord and returns the raw JSON body.
func (c *CrUX) QueryRecord(ctx context.Context, req CruxRequest) (json.RawMessage, error) {
	return c.post(ctx, cruxQueryURL, req)
}

// QueryHistoryRecord calls :queryHistoryRecord and returns the raw JSON body.
func (c *CrUX) QueryHistoryRecord(ctx context.Context, req CruxRequest) (json.RawMessage, error) {
	return c.post(ctx, cruxHistoryURL, req)
}

func (c *CrUX) post(ctx context.Context, url string, body any) (json.RawMessage, error) {
	if c.APIKey == "" {
		return nil, errs.New(errs.CodeAuthRequired, "CrUX API key required").WithHint("Set GSC_CRUX_API_KEY or run `gsc config set crux.api_key <key>`. Get a key at https://console.cloud.google.com/apis/credentials after enabling the Chrome UX Report API: https://console.cloud.google.com/apis/library/chromeuxreport.googleapis.com")
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, errs.New(errs.CodeGeneric, err.Error())
	}
	url = url + "?key=" + c.APIKey
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, errs.New(errs.CodeGeneric, err.Error())
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(httpReq)
	if err != nil {
		m := err.Error()
		if strings.Contains(m, "no such host") || strings.Contains(m, "dial tcp") {
			return nil, errs.New(errs.CodeNetworkUnreachable, m)
		}
		return nil, errs.New(errs.CodeGeneric, m)
	}
	defer resp.Body.Close()
	payload, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return payload, nil
	}
	var env cruxErrorEnvelope
	_ = json.Unmarshal(payload, &env)
	msg := env.Err.Message
	if msg == "" {
		msg = fmt.Sprintf("crux http %d", resp.StatusCode)
	}
	status := strings.ToUpper(env.Err.Status)
	switch {
	case resp.StatusCode == 401:
		return nil, errs.New(errs.CodeAuthExpired, msg).WithHint("Run `gsc auth login`.")
	case resp.StatusCode == 403 && (strings.Contains(status, "SERVICE_DISABLED") || strings.Contains(status, "PERMISSION_DENIED") || strings.Contains(strings.ToLower(msg), "has not been used") || strings.Contains(strings.ToLower(msg), "disabled")):
		return nil, errs.New(errs.CodeAuthRequired, msg).WithHint("Enable the Chrome UX Report API for the GCP project backing your credentials: https://console.cloud.google.com/apis/library/chromeuxreport.googleapis.com")
	case resp.StatusCode == 403:
		return nil, errs.New(errs.CodeAuthDenied, msg)
	case resp.StatusCode == 404:
		return nil, errs.New(errs.CodeNotFound, msg).WithHint("No CrUX data for this URL. Try the origin: gsc crux query <origin> --origin")
	case resp.StatusCode == 429:
		return nil, errs.New(errs.CodeRateLimited, msg).WithRetry(60).WithHint("Run `gsc quota` to inspect the `crux` bucket usage.")
	case resp.StatusCode >= 500:
		return nil, errs.New(errs.CodeAPI5xx, msg).WithRetry(30)
	case resp.StatusCode >= 400:
		return nil, errs.New(errs.CodeInvalidArgs, msg)
	}
	return nil, errs.New(errs.CodeGeneric, msg)
}
