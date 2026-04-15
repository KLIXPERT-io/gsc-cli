// Package output handles JSON / CSV / table rendering per FR-1.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"time"

	"golang.org/x/term"
)

type Format string

const (
	FormatJSON  Format = "json"
	FormatCSV   Format = "csv"
	FormatTable Format = "table"
)

// Meta is the envelope metadata included on every JSON response per FR-1.
type Meta struct {
	Cached         bool   `json:"cached"`
	CachedAt       string `json:"cached_at,omitempty"` // RFC3339 or empty
	TTLRemainingSec *int  `json:"ttl_remaining_sec"`
	APICalls       int    `json:"api_calls"`
	Partial        bool   `json:"partial,omitempty"`
}

type Envelope struct {
	Data any  `json:"data"`
	Meta Meta `json:"meta"`
}

// ResolveFormat returns the requested format or auto-detects based on TTY.
func ResolveFormat(flag string, stdoutFd uintptr) Format {
	if flag != "" {
		return Format(flag)
	}
	if term.IsTerminal(int(stdoutFd)) {
		return FormatTable
	}
	return FormatJSON
}

// IsTTY reports whether fd is a terminal.
func IsTTY(fd uintptr) bool { return term.IsTerminal(int(fd)) }

// WriteJSON writes the envelope as pretty JSON.
func WriteJSON(w io.Writer, data any, meta Meta) error {
	env := Envelope{Data: data, Meta: meta}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(env)
}

// Row is a generic row for CSV/Table rendering.
type Row = map[string]any

// WriteCSV writes rows with a header row in the given column order.
func WriteCSV(w io.Writer, columns []string, rows []Row) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(columns); err != nil {
		return err
	}
	for _, r := range rows {
		rec := make([]string, len(columns))
		for i, c := range columns {
			rec[i] = fmtCell(r[c])
		}
		if err := cw.Write(rec); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

// WriteTable writes a simple aligned table.
func WriteTable(w io.Writer, columns []string, rows []Row) error {
	widths := make([]int, len(columns))
	for i, c := range columns {
		widths[i] = len(c)
	}
	data := make([][]string, len(rows))
	for i, r := range rows {
		rec := make([]string, len(columns))
		for j, c := range columns {
			s := fmtCell(r[c])
			rec[j] = s
			if len(s) > widths[j] {
				widths[j] = len(s)
			}
		}
		data[i] = rec
	}
	writeRow(w, columns, widths)
	sep := make([]string, len(columns))
	for i, wd := range widths {
		sep[i] = pad("", wd, '-')
	}
	writeRaw(w, sep, widths)
	for _, rec := range data {
		writeRow(w, rec, widths)
	}
	return nil
}

func writeRow(w io.Writer, rec []string, widths []int) {
	for i, s := range rec {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprint(w, pad(s, widths[i], ' '))
	}
	fmt.Fprintln(w)
}

func writeRaw(w io.Writer, rec []string, widths []int) {
	for i, s := range rec {
		if i > 0 {
			fmt.Fprint(w, "  ")
		}
		fmt.Fprint(w, pad(s, widths[i], '-'))
	}
	fmt.Fprintln(w)
}

func pad(s string, n int, ch rune) string {
	if len(s) >= n {
		return s
	}
	p := make([]rune, n-len(s))
	for i := range p {
		p[i] = ch
	}
	return s + string(p)
}

func fmtCell(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	case time.Time:
		return x.Format(time.RFC3339)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// MetaFromCache builds a Meta entry given cache info.
func MetaFromCache(cached bool, cachedAt time.Time, ttlRemaining time.Duration, apiCalls int) Meta {
	m := Meta{Cached: cached, APICalls: apiCalls}
	if cached {
		m.CachedAt = cachedAt.Format(time.RFC3339)
		sec := int(ttlRemaining.Seconds())
		m.TTLRemainingSec = &sec
	}
	return m
}

// Stdout returns os.Stdout (helper for tests).
func Stdout() io.Writer { return os.Stdout }
