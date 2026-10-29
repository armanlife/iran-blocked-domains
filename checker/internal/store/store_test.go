package store

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/armanlife/iran-blocked-domains/checker/internal/check"
)

var ctx = context.Background()

func openStore(t *testing.T, domains ...string) *Store {
	t.Helper()
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })

	im, err := s.NewImporter()
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range domains {
		if err := im.Add(d, "test"); err != nil {
			t.Fatal(err)
		}
	}
	if err := im.Close(); err != nil {
		t.Fatal(err)
	}
	return s
}

func result(domain string, c check.Category) check.Result {
	return check.Result{Domain: domain, Status: c.Status(), Method: c.Method(), Category: c}
}

func collect(t *testing.T, sel *Selection) []string {
	t.Helper()
	var got []string
	for _, d := range sel.All(ctx) {
		got = append(got, d)
	}
	if err := sel.Err(); err != nil {
		t.Fatal(err)
	}
	return got
}

func readLines(t *testing.T, path string) []string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Fields(string(data))
}

func TestImporterSkipsDuplicates(t *testing.T) {
	s := openStore(t, "a.com", "b.com", "a.com", "c.com")

	if n, _ := s.DomainCount(); n != 3 {
		t.Errorf("domains = %d, want 3", n)
	}
	f, err := s.Find(ctx, "c.com")
	if err != nil || f == nil || f.Rank != 3 {
		t.Errorf("c.com = %+v, %v; want rank 3", f, err)
	}
}

func TestPendingResumesInRankOrder(t *testing.T) {
	var domains []string
	for i := range 2500 { // More than a page
		domains = append(domains, "d"+string(rune('a'+i%26))+strings.Repeat("x", i/26)+".com")
	}
	s := openStore(t, domains...)

	w, err := s.NewWriter()
	if err != nil {
		t.Fatal(err)
	}
	// Results of some domains, out of order, like after a stop
	for _, rank := range []int{3, 1, 2000, 7} {
		w.Add(rank, result(domains[rank-1], check.CategoryNotBlocked))
	}
	if err := w.Flush(); err != nil {
		t.Fatal(err)
	}

	got := collect(t, s.Pending())
	if len(got) != 2496 || got[0] != domains[1] || got[1] != domains[3] {
		t.Fatalf("pending = %d domains starting %v", len(got), got[:2])
	}
	if !slices.IsSortedFunc(got, func(a, b string) int { return slices.Index(domains, a) - slices.Index(domains, b) }) {
		t.Error("pending domains are not in rank order")
	}
	if n, _ := s.Pending().Count(ctx); n != 2496 {
		t.Errorf("count = %d, want 2496", n)
	}
}

func TestRecheckKeepsHistory(t *testing.T) {
	s := openStore(t, "a.com", "b.com", "c.com", "d.com")
	w, _ := s.NewWriter()
	w.Add(1, result("a.com", check.CategoryDNSTimeout))
	w.Add(2, result("b.com", check.CategoryBlockedDNS))
	w.Add(3, result("c.com", check.CategoryDNSTimeout))
	w.Add(4, result("d.com", check.CategoryConnectFailed))
	w.Flush()
	time.Sleep(5 * time.Millisecond) // Results are timed in milliseconds

	r := Recheck{Categories: []check.Category{check.CategoryDNSTimeout, check.CategoryConnectFailed}, Since: time.Now()}
	if got := collect(t, s.Recheck(r)); !slices.Equal(got, []string{"a.com", "c.com", "d.com"}) {
		t.Fatalf("recheck = %v", got)
	}

	// c.com is rechecked and now blocked, then the recheck stops
	w.Add(3, result("c.com", check.CategoryBlockedSNI))
	w.Flush()
	if got := collect(t, s.Recheck(r)); !slices.Equal(got, []string{"a.com", "d.com"}) {
		t.Errorf("recheck after a stop = %v", got)
	}

	f, _ := s.Find(ctx, "c.com")
	if f.Result.Category != check.CategoryBlockedSNI || len(f.History) != 1 || f.History[0].Category != check.CategoryDNSTimeout {
		t.Errorf("c.com = %+v, history %+v", f.Result, f.History)
	}

	summary := w.Summary()
	if summary.Total != 4 || summary.Blocked != 2 || summary.ByCategory[check.CategoryDNSTimeout] != 1 {
		t.Errorf("summary = %+v", summary)
	}
	if counts, _ := s.Counts(); counts.Blocked != 2 || counts.Total != 4 {
		t.Errorf("counts = %+v", counts)
	}
}

func TestExportInRankOrder(t *testing.T) {
	s := openStore(t, "a.com", "b.com", "c.com", "d.com", "e.com")
	w, _ := s.NewWriter()
	w.Add(5, result("e.com", check.CategoryBlockedDNS))
	w.Add(2, result("b.com", check.CategoryBlockedSNI))
	w.Add(4, result("d.com", check.CategoryBlockedDNS))
	w.Add(1, result("a.com", check.CategoryNotBlocked))
	w.Flush()

	if err := s.Export(ctx, Status{Total: 5, Checked: 4}, true); err != nil {
		t.Fatal(err)
	}

	if got := readLines(t, filepath.Join(s.Dir, BlockedFile)); !slices.Equal(got, []string{"b.com", "d.com", "e.com"}) {
		t.Errorf("blocked.txt = %v", got)
	}
	if got := readLines(t, filepath.Join(s.Dir, "blocked-dns.txt")); !slices.Equal(got, []string{"d.com", "e.com"}) {
		t.Errorf("blocked-dns.txt = %v", got)
	}
	// Every category has a file, even when empty
	if got := readLines(t, filepath.Join(s.Dir, "tls-failed.txt")); len(got) != 0 {
		t.Errorf("tls-failed.txt = %v", got)
	}
	if got := readLines(t, filepath.Join(s.Dir, ".gitignore")); !slices.Contains(got, "not-blocked.txt") || !slices.Contains(got, "connect-failed.txt") || !slices.Contains(got, DBFile) {
		t.Errorf(".gitignore = %v", got)
	}
	if status, err := ReadStatus(s.Dir); err != nil || status.Checked != 4 {
		t.Errorf("run.json = %+v, %v", status, err)
	}
}

func TestWriteData(t *testing.T) {
	dir := t.TempDir()
	blocked := filepath.Join(dir, "blocked.txt")
	os.WriteFile(blocked, []byte("a.com\nb.com\n\nc.com\n"), 0o644)
	data := filepath.Join(dir, "data")

	domains, err := WriteData(blocked, data, "list")
	if err != nil {
		t.Fatal(err)
	}
	if domains != 3 {
		t.Errorf("wrote %d domains, want 3", domains)
	}
	if got := readLines(t, filepath.Join(data, "list")); !slices.Equal(got, []string{"a.com", "b.com", "c.com"}) {
		t.Errorf("list = %v", got)
	}
}

func TestSettingsAndRecheckMeta(t *testing.T) {
	s := openStore(t)
	started := time.Now().Truncate(time.Second)
	want := Settings{List: "host10m", DNS: "1.1.1.1", ControlSNI: "example.com", Timeout: 5 * time.Second, Started: started}
	if err := s.SaveSettings(want); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Settings(); err != nil || got != want {
		t.Errorf("settings = %+v, %v; want %+v", got, err, want)
	}

	r := &Recheck{Categories: []check.Category{check.CategoryDNSTimeout}, Since: time.UnixMilli(1234567)}
	s.SetRecheck(r)
	if got, _ := s.CurrentRecheck(); got == nil || !got.Since.Equal(r.Since) || !slices.Equal(got.Categories, r.Categories) {
		t.Errorf("recheck = %+v", got)
	}
	s.SetRecheck(nil)
	if got, _ := s.CurrentRecheck(); got != nil {
		t.Errorf("recheck after clearing = %+v", got)
	}
}

func TestParseCategories(t *testing.T) {
	got, err := ParseCategories("dns-timeout, connect-failed")
	if err != nil || !slices.Equal(got, []check.Category{check.CategoryDNSTimeout, check.CategoryConnectFailed}) {
		t.Errorf("got %v, %v", got, err)
	}
	if _, err := ParseCategories("timeout"); err == nil {
		t.Error("unknown category accepted")
	}
}
