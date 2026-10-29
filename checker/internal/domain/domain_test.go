package domain

import (
	"slices"
	"strings"
	"testing"
)

func TestNormalize(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"x.com", "x.com", true},
		{"  X.COM  ", "x.com", true},
		{`"x.com"`, "x.com", true},
		{"https://www.Example.com:443/path?q=1", "www.example.com", true},
		{"full:example.com", "example.com", true},
		{"domain:example.com @ads", "example.com", true},
		{"example.com.", "example.com", true},
		{"xn--mgbaam7a8h.xn--mgbai9azgqp6j", "xn--mgbaam7a8h.xn--mgbai9azgqp6j", true},
		{"localhost", "localhost", false},
		{"-bad.com", "-bad.com", false},
		{"bad..com", "bad..com", false},
		{"under_score.com", "under_score.com", false},
	}

	for _, tt := range tests {
		got, ok := Normalize(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("Normalize(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestParseLine(t *testing.T) {
	tests := []struct {
		in   string
		want string
		ok   bool
	}{
		{"google.com,1", "google.com", true},
		{"1,google.com", "google.com", true},
		{`"1";"google.com";"5.2"`, "google.com", true},
		{"google.com\t1", "google.com", true},
		{"domain,rank", "", false},
	}

	for _, tt := range tests {
		got, ok := ParseLine(tt.in)
		if got != tt.want || ok != tt.ok {
			t.Errorf("ParseLine(%q) = %q, %v; want %q, %v", tt.in, got, ok, tt.want, tt.ok)
		}
	}
}

func TestReader(t *testing.T) {
	input := "domain,rank\nx.com,1\n\n# comment\nfacebook.com,2 # inline comment\r\nnot a domain\n"

	r := NewReader(strings.NewReader(input))
	domains := slices.Collect(r.All())
	if err := r.Err(); err != nil {
		t.Fatal(err)
	}
	if want := []string{"x.com", "facebook.com"}; !slices.Equal(domains, want) {
		t.Errorf("domains = %v, want %v", domains, want)
	}
	if r.Invalid != 2 {
		t.Errorf("invalid = %d, want 2", r.Invalid)
	}
}

func TestParse(t *testing.T) {
	domains, invalid := Parse("x.com, facebook.com\ndigikala.com  X.com\tbad")

	if want := []string{"x.com", "facebook.com", "digikala.com"}; !slices.Equal(domains, want) {
		t.Errorf("domains = %v, want %v", domains, want)
	}
	if want := []string{"bad"}; !slices.Equal(invalid, want) {
		t.Errorf("invalid = %v, want %v", invalid, want)
	}
}
