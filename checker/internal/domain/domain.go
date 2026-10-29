// Package domain reads and normalizes domain lists.
package domain

import (
	"bufio"
	"io"
	"iter"
	"os"
	"strings"
)

// Reader streams domains from a list, one per line, so files with
// millions of domains are never loaded into memory.
//
// Lines can be plain domains, URLs, v2fly rules or CSV rows such as
// "google.com,1" or "1,google.com". Empty lines and comments (#) are skipped.
type Reader struct {
	scanner *bufio.Scanner
	// Invalid is the number of lines without a valid domain
	Invalid int
}

func NewReader(r io.Reader) *Reader {
	return &Reader{scanner: bufio.NewScanner(r)}
}

// All iterates over the domains. Check Err after the loop.
func (r *Reader) All() iter.Seq[string] {
	return func(yield func(string) bool) {
		for r.scanner.Scan() {
			line := stripComment(r.scanner.Text())
			if line == "" {
				continue
			}

			domain, ok := ParseLine(line)
			if !ok {
				r.Invalid++
				continue
			}
			if !yield(domain) {
				return
			}
		}
	}
}

func (r *Reader) Err() error {
	return r.scanner.Err()
}

// Numbered iterates over domains with their position, starting at 1
func Numbered(domains iter.Seq[string]) iter.Seq2[int, string] {
	return func(yield func(int, string) bool) {
		i := 0
		for domain := range domains {
			i++
			if !yield(i, domain) {
				return
			}
		}
	}
}

// Count returns the number of valid domains and invalid lines in a list file
func Count(path string) (valid, invalid int, err error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()

	r := NewReader(f)
	for range r.All() {
		valid++
	}
	return valid, r.Invalid, r.Err()
}

// ParseLine returns the first field of a line that is a valid domain
func ParseLine(line string) (string, bool) {
	fields := strings.FieldsFunc(stripComment(line), func(r rune) bool {
		return r == ',' || r == ';' || r == '\t'
	})
	for _, field := range fields {
		if domain, ok := Normalize(field); ok {
			return domain, true
		}
	}
	return "", false
}

// Parse reads domains separated by new lines, spaces or commas.
// Duplicates are removed and invalid entries are returned in invalid.
func Parse(text string) (domains, invalid []string) {
	seen := make(map[string]bool)
	fields := strings.FieldsFunc(text, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
	})

	for _, field := range fields {
		domain, ok := Normalize(field)
		if !ok {
			invalid = append(invalid, field)
			continue
		}
		if !seen[domain] {
			seen[domain] = true
			domains = append(domains, domain)
		}
	}
	return domains, invalid
}

// Normalize turns inputs like "https://WWW.Example.com:443/path"
// or "full:example.com" into a plain domain name
func Normalize(s string) (string, bool) {
	s = strings.ToLower(strings.Trim(strings.TrimSpace(s), `"'`))

	// v2fly domain-list-community rules
	s = strings.TrimPrefix(s, "domain:")
	s = strings.TrimPrefix(s, "full:")
	// Attributes such as "@ads"
	s, _, _ = strings.Cut(s, " ")

	// URLs
	if _, rest, ok := strings.Cut(s, "://"); ok {
		s = rest
	}
	s, _, _ = strings.Cut(s, "/")
	s, _, _ = strings.Cut(s, ":")
	s = strings.TrimSuffix(s, ".")

	return s, isValid(s)
}

func stripComment(line string) string {
	line, _, _ = strings.Cut(line, "#")
	return strings.TrimSpace(line)
}

func isValid(s string) bool {
	if len(s) > 253 {
		return false
	}

	labels := strings.Split(s, ".")
	if len(labels) < 2 {
		return false
	}

	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, ch := range label {
			if !(ch >= 'a' && ch <= 'z' || ch >= '0' && ch <= '9' || ch == '-') {
				return false
			}
		}
	}
	return true
}
