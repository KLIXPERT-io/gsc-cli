package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/KLIXPERT-io/gsc-cli/internal/errs"
)

// cruxAPIKey resolves the CrUX API key from env (preferred) or config.
func cruxAPIKey(s *State) string {
	if v := strings.TrimSpace(os.Getenv("GSC_CRUX_API_KEY")); v != "" {
		return v
	}
	if s != nil && s.Cfg != nil {
		return strings.TrimSpace(s.Cfg.CrUX.APIKey)
	}
	return ""
}

// psiAPIKey resolves the PSI API key from env (preferred) or config.
// PSI accepts both OAuth and API key; key is optional but unauth'd PSI is rate-limited.
func psiAPIKey(s *State) string {
	if v := strings.TrimSpace(os.Getenv("GSC_PSI_API_KEY")); v != "" {
		return v
	}
	if s != nil && s.Cfg != nil {
		return strings.TrimSpace(s.Cfg.PSI.APIKey)
	}
	return ""
}

// cwvMetricOrder is the canonical display order for CWV metrics.
func cwvMetricOrder() []string { return []string{"lcp", "inp", "cls", "ttfb"} }

// cwvCruxKey maps our short metric name to the CrUX API metric key.
func cwvCruxKey(m string) (string, bool) {
	switch m {
	case "lcp":
		return "largest_contentful_paint", true
	case "inp":
		return "interaction_to_next_paint", true
	case "cls":
		return "cumulative_layout_shift", true
	case "ttfb":
		return "experimental_time_to_first_byte", true
	case "fcp":
		return "first_contentful_paint", true
	}
	return "", false
}

// cwvPSIKey maps our short metric name to the PSI loadingExperience key.
func cwvPSIKey(m string) (string, bool) {
	switch m {
	case "lcp":
		return "LARGEST_CONTENTFUL_PAINT_MS", true
	case "inp":
		return "INTERACTION_TO_NEXT_PAINT", true
	case "cls":
		return "CUMULATIVE_LAYOUT_SHIFT_SCORE", true
	case "ttfb":
		return "EXPERIMENTAL_TIME_TO_FIRST_BYTE", true
	case "fcp":
		return "FIRST_CONTENTFUL_PAINT_MS", true
	}
	return "", false
}

// rateCWV rates a p75 value. Units: ms for lcp/inp/ttfb, unitless for cls.
func rateCWV(metric string, v float64) string {
	switch metric {
	case "lcp":
		switch {
		case v <= 2500:
			return "good"
		case v <= 4000:
			return "needs-improvement"
		default:
			return "poor"
		}
	case "inp":
		switch {
		case v <= 200:
			return "good"
		case v <= 500:
			return "needs-improvement"
		default:
			return "poor"
		}
	case "cls":
		switch {
		case v <= 0.1:
			return "good"
		case v <= 0.25:
			return "needs-improvement"
		default:
			return "poor"
		}
	case "ttfb":
		switch {
		case v <= 800:
			return "good"
		case v <= 1800:
			return "needs-improvement"
		default:
			return "poor"
		}
	}
	return ""
}

// formatCWVValue renders a p75 value for display.
func formatCWVValue(metric string, v float64) string {
	if metric == "cls" {
		return strconv.FormatFloat(v, 'f', 2, 64)
	}
	return fmt.Sprintf("%.0fms", v)
}

// parseCruxP75 parses the p75 field which the CrUX API returns as string or number.
func parseCruxP75(raw json.RawMessage) (float64, bool) {
	if len(raw) == 0 {
		return 0, false
	}
	// number
	var n float64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n, true
	}
	// string
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		f, err := strconv.ParseFloat(s, 64)
		if err == nil {
			return f, true
		}
	}
	return 0, false
}

var allFormFactors = []string{"phone", "desktop", "tablet", "all"}

// normalizeFormFactor returns the API enum (PHONE|DESKTOP|TABLET) or "" for ALL.
// Accepts "phone", "desktop", "tablet", "all" (case-insensitive).
func normalizeFormFactor(ff string) (string, error) {
	ff = strings.ToLower(strings.TrimSpace(ff))
	switch ff {
	case "", "all":
		return "", nil
	case "phone":
		return "PHONE", nil
	case "desktop":
		return "DESKTOP", nil
	case "tablet":
		return "TABLET", nil
	}
	return "", errs.New(errs.CodeInvalidArgs, "invalid --form-factor: "+ff).WithHint("accepted: " + strings.Join(allFormFactors, ", "))
}

var allCWVMetrics = []string{"lcp", "inp", "cls", "ttfb", "fcp"}

// normalizeMetrics maps user-supplied metric tokens to CrUX API keys (returns the CrUX keys list, or nil for "all").
func normalizeMetrics(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, nil
	}
	var out []string
	for _, s := range in {
		for _, p := range strings.Split(s, ",") {
			p = strings.ToLower(strings.TrimSpace(p))
			if p == "" {
				continue
			}
			k, ok := cwvCruxKey(p)
			if !ok {
				return nil, errs.New(errs.CodeInvalidArgs, "invalid --metric: "+p).WithHint("accepted: " + strings.Join(allCWVMetrics, ", "))
			}
			out = append(out, k)
		}
	}
	return out, nil
}

// isBareOrigin reports whether s is of the form scheme://host[:port] with no path/query/fragment.
func isBareOrigin(s string) bool {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return false
	}
	return (u.Path == "" || u.Path == "/") && u.RawQuery == "" && u.Fragment == ""
}

// originOf returns scheme://host[:port] for a URL input.
func originOf(s string) (string, error) {
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", errs.New(errs.CodeInvalidArgs, "invalid URL: "+s)
	}
	return u.Scheme + "://" + u.Host, nil
}

// cruxRatingFromMetric looks up the short metric (lcp/inp/cls/ttfb/fcp) for a CrUX API key.
func cruxShortForKey(apiKey string) string {
	for _, m := range allCWVMetrics {
		if k, _ := cwvCruxKey(m); k == apiKey {
			return m
		}
	}
	return apiKey
}
