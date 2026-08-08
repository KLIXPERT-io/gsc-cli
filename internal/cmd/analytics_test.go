package cmd

import (
	"testing"

	searchconsole "google.golang.org/api/searchconsole/v1"
)

func TestParseFilter(t *testing.T) {
	cases := []struct {
		in   string
		dim  string
		op   string
		expr string
	}{
		{"device=MOBILE", "device", "equals", "MOBILE"},
		{"country!=usa", "country", "notEquals", "usa"},
		{"query~buy", "query", "contains", "buy"},
		{"query!~free", "query", "notContains", "free"},
		{"page~~/(blog|guides)/", "page", "includingRegex", "/(blog|guides)/"},
		{"page!~~^/tag/", "page", "excludingRegex", "^/tag/"},
		// The separator is the earliest operator, so operator characters inside
		// the value stay part of the value.
		{"query=buy~now", "query", "equals", "buy~now"},
		{"query~a~~b", "query", "contains", "a~~b"},
		{"page~~^/a=b", "page", "includingRegex", "^/a=b"},
	}
	for _, c := range cases {
		got, err := parseFilter(c.in)
		if err != nil {
			t.Errorf("parseFilter(%q): unexpected error: %v", c.in, err)
			continue
		}
		if got.Dimension != c.dim || got.Operator != c.op || got.Expression != c.expr {
			t.Errorf("parseFilter(%q) = (%q, %q, %q), want (%q, %q, %q)",
				c.in, got.Dimension, got.Operator, got.Expression, c.dim, c.op, c.expr)
		}
	}
}

func TestParseFilterRejectsMalformed(t *testing.T) {
	for _, in := range []string{"device", "=MOBILE", "~buy", ""} {
		if _, err := parseFilter(in); err == nil {
			t.Errorf("parseFilter(%q): expected error, got nil", in)
		}
	}
}

func TestSortRows(t *testing.T) {
	rows := func() []*searchconsole.ApiDataRow {
		return []*searchconsole.ApiDataRow{
			{Keys: []string{"a"}, Clicks: 5, Impressions: 10, Ctr: 0.5, Position: 3},
			{Keys: []string{"b"}, Clicks: 1, Impressions: 90, Ctr: 0.01, Position: 1},
			{Keys: []string{"c"}, Clicks: 9, Impressions: 50, Ctr: 0.18, Position: 7},
		}
	}
	cases := []struct {
		orderBy string
		asc     bool
		want    []string
	}{
		{"clicks", false, []string{"c", "a", "b"}},
		{"clicks", true, []string{"b", "a", "c"}},
		{"impressions", false, []string{"b", "c", "a"}},
		{"ctr", false, []string{"a", "c", "b"}},
		{"position", true, []string{"b", "a", "c"}},
	}
	for _, c := range cases {
		r := rows()
		sortRows(r, c.orderBy, c.asc)
		for i, want := range c.want {
			if r[i].Keys[0] != want {
				t.Errorf("sortRows(%s, asc=%v) = %s at %d, want %s",
					c.orderBy, c.asc, r[i].Keys[0], i, want)
				break
			}
		}
	}
}

func TestValidateDataState(t *testing.T) {
	for _, ok := range []string{"final", "all", "hourly_all"} {
		if err := validateDataState(ok); err != nil {
			t.Errorf("validateDataState(%q): unexpected error: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "hourly", "HOURLY_ALL", "fresh"} {
		if err := validateDataState(bad); err == nil {
			t.Errorf("validateDataState(%q): expected error, got nil", bad)
		}
	}
}
