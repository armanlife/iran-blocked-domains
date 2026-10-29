package store

import (
	"os"
	"path/filepath"
	"slices"
)

// RunInfo describes a run folder, from its run.json
type RunInfo struct {
	Dir    string
	Status Status
}

// Runs returns the run folders in resultsDir, most recently updated first
func Runs(resultsDir string) []RunInfo {
	entries, err := os.ReadDir(resultsDir)
	if err != nil {
		return nil
	}

	var runs []RunInfo
	for _, entry := range entries {
		dir := filepath.Join(resultsDir, entry.Name())
		if !entry.IsDir() || !Exists(dir) {
			continue
		}
		status, err := ReadStatus(dir)
		if err != nil {
			continue
		}
		runs = append(runs, RunInfo{Dir: dir, Status: status})
	}

	slices.SortFunc(runs, func(a, b RunInfo) int { return b.Status.Updated.Compare(a.Status.Updated) })
	return runs
}
