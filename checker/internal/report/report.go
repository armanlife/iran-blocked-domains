// Package report writes check results as text, CSV, JSON or a plain domain list.
package report

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
)

type Writer interface {
	Write(check.Result) error
}

// Formats lists the supported formats and their file extensions
var Formats = map[string]string{
	"text": ".txt",
	"csv":  ".csv",
	"json": ".json",
	"list": ".txt",
}

// CSVHeader is the first row of CSV results
var CSVHeader = []string{"domain", "status", "method", "category", "ips", "reason"}

// CSVRecord returns r as a CSV row
func CSVRecord(r check.Result) []string {
	return []string{r.Domain, string(r.Status), string(r.Method), string(r.Category), strings.Join(r.IPs, " "), r.Reason}
}

func New(w io.Writer, format string) (Writer, error) {
	switch format {
	case "text":
		return &textWriter{w: w}, nil
	case "csv":
		cw := csv.NewWriter(w)
		if err := cw.Write(CSVHeader); err != nil {
			return nil, err
		}
		return &csvWriter{w: cw}, nil
	case "json":
		return &jsonWriter{enc: json.NewEncoder(w)}, nil
	case "list":
		return &listWriter{w: w}, nil
	default:
		return nil, fmt.Errorf("unknown format %q (use text, csv, json or list)", format)
	}
}

type textWriter struct {
	w io.Writer
}

func (t *textWriter) Write(r check.Result) error {
	_, err := fmt.Fprintf(t.w, "%-32s %-12s %-14s %-32s %s\n",
		r.Domain, r.Status, r.Category, strings.Join(r.IPs, ","), r.Reason)
	return err
}

type csvWriter struct {
	w *csv.Writer
}

func (c *csvWriter) Write(r check.Result) error {
	if err := c.w.Write(CSVRecord(r)); err != nil {
		return err
	}
	c.w.Flush()
	return c.w.Error()
}

// jsonWriter writes one JSON object per line (JSON Lines)
type jsonWriter struct {
	enc *json.Encoder
}

func (j *jsonWriter) Write(r check.Result) error {
	return j.enc.Encode(r)
}

// listWriter writes blocked domains only, one per line, like the files in data/
type listWriter struct {
	w io.Writer
}

func (l *listWriter) Write(r check.Result) error {
	if r.Status != check.StatusBlocked {
		return nil
	}
	_, err := fmt.Fprintln(l.w, r.Domain)
	return err
}
