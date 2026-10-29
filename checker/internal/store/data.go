package store

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// WriteData writes the blocked list for releases to dataDir/name, in rank order.
// It reads blocked.txt, so it also works on published results without the database.
func WriteData(blockedFile, dataDir, name string) (domains int, err error) {
	data, err := os.ReadFile(blockedFile)
	if err != nil {
		return 0, err
	}
	lines := strings.Fields(string(data))
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return 0, err
	}
	if err := writeLines(filepath.Join(dataDir, name), lines); err != nil {
		return 0, err
	}
	return len(lines), nil
}

func writeLines(path string, lines []string) error {
	return writeAtomic(path, func(w *bufio.Writer) error {
		for _, line := range lines {
			w.WriteString(line)
			w.WriteByte('\n')
		}
		return nil
	})
}
