package cmd

import (
	"github.com/KLIXPERT-io/gsc-cli/internal/output"
	"github.com/KLIXPERT-io/gsc-cli/internal/quota"
	"github.com/spf13/cobra"
)

func newQuotaCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "quota",
		Short: "Show today's API usage against known limits",
		Long: `Prints current counters tracked at ~/.config/gsc/quota.json. Resets at midnight
America/Los_Angeles (GSC quota window).

Examples:
  gsc quota
  gsc quota --output json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			s := getState(cmd)
			c, err := s.Quota.Load()
			if err != nil {
				return err
			}
			data := map[string]any{
				"date": c.Date,
				"url_inspection": map[string]any{
					"used":  c.URLInspection,
					"limit": quota.URLInspectionDailyLimit,
				},
				"search_analytics": map[string]any{
					"used_today":     c.SearchAnalytics,
					"rate_limit_qpm": quota.SearchAnalyticsQPM,
				},
				"psi": map[string]any{
					"used":  c.PSI,
					"limit": quota.PSIDailyLimit,
				},
				"crux": map[string]any{
					"used": c.CRUX,
					"note": "150 QPS best-effort (no daily limit)",
				},
				"other": c.Other,
			}
			cols := []string{"category", "used", "limit"}
			rows := []output.Row{
				{"category": "url_inspection", "used": c.URLInspection, "limit": quota.URLInspectionDailyLimit},
				{"category": "search_analytics", "used": c.SearchAnalytics, "limit": quota.SearchAnalyticsQPM},
				{"category": "psi", "used": c.PSI, "limit": quota.PSIDailyLimit},
				{"category": "crux", "used": c.CRUX, "limit": "—"},
				{"category": "other", "used": c.Other, "limit": "—"},
			}
			return emit(cmd, data, output.Meta{APICalls: 0}, cols, rows)
		},
	}
}
