package merge

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type recorder struct {
	domains, sources []string
	seen             map[string]bool
}

// Add skips duplicates like store.Importer
func (r *recorder) Add(domain, source string) error {
	if r.seen[domain] {
		return nil
	}
	r.seen[domain] = true
	r.domains = append(r.domains, domain)
	r.sources = append(r.sources, source)
	return nil
}

func writeFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestMerge(t *testing.T) {
	ranking := writeFile(t, "host10m.csv", "google.com,1\nfacebook.com,2\ngoogleapis.com,3\nyoutube.com,4\nx.com,5\n")
	previous := writeFile(t, "ir-blocked-domain",
		"cdn.example.org\n"+ // Before any ranked domain
			"youtube.com\ngooglevideo.com\n"+ // After youtube.com
			"googleapis.com\nytimg.com\nfbcdn.net\n"+ // After googleapis.com, in this order
			"m.facebook.com\n"+ // After its parent
			"youtube.com\n") // Duplicate

	r := &recorder{seen: make(map[string]bool)}
	stats, err := Merge(ranking, previous, r, nil)
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"cdn.example.org",
		"google.com",
		"facebook.com", "m.facebook.com",
		"googleapis.com", "ytimg.com", "fbcdn.net",
		"youtube.com", "googlevideo.com",
		"x.com",
	}
	if !slices.Equal(r.domains, want) {
		t.Errorf("merged =\n%v\nwant\n%v", r.domains, want)
	}
	if r.sources[0] != "ir-blocked-domain" || r.sources[1] != "host10m" {
		t.Errorf("sources = %v", r.sources)
	}

	wantStats := Stats{Ranking: 5, Previous: 7, PreviousInRanking: 2, AfterParent: 1, AfterNeighbor: 3, AtTop: 1}
	if stats != wantStats {
		t.Errorf("stats = %+v, want %+v", stats, wantStats)
	}
}

func TestParents(t *testing.T) {
	if got := parents("a.b.example.co"); !slices.Equal(got, []string{"b.example.co", "example.co"}) {
		t.Errorf("parents = %v", got)
	}
	if got := parents("example.com"); len(got) != 0 {
		t.Errorf("parents = %v", got)
	}
}
