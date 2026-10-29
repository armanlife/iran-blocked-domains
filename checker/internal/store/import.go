package store

import (
	"database/sql"
	"errors"
	"strings"
)

const (
	importRows  = 500    // Rows per INSERT statement
	importBatch = 100000 // Rows per transaction
)

// Importer loads the ranked list of a new run folder. For speed the domains
// are inserted without their index, which is built once at the end; duplicates
// are then removed, so each domain keeps its first rank.
type Importer struct {
	s     *Store
	tx    *sql.Tx
	rows  []any
	next  int // Rank of the next domain
	inTx  int // Rows in the current transaction
	added int

	// Added and Duplicates are set by Close
	Added, Duplicates int
}

func (s *Store) NewImporter() (*Importer, error) {
	n, err := s.DomainCount()
	if err != nil {
		return nil, err
	}
	if n > 0 {
		return nil, errors.New("the run folder already has its list")
	}
	return &Importer{s: s, next: 1}, nil
}

// Add appends domain with the given source
func (im *Importer) Add(domain, source string) error {
	im.rows = append(im.rows, im.next, domain, source)
	im.next++
	if len(im.rows) >= importRows*3 {
		return im.insert()
	}
	return nil
}

// Count returns the number of domains added so far, duplicates included
func (im *Importer) Count() int {
	return im.next - 1
}

func (im *Importer) insert() error {
	if len(im.rows) == 0 {
		return nil
	}
	if im.tx == nil {
		tx, err := im.s.db.Begin()
		if err != nil {
			return err
		}
		im.tx = tx
	}

	n := len(im.rows) / 3
	query := `INSERT INTO domains (rank, domain, source) VALUES ` + strings.TrimSuffix(strings.Repeat("(?, ?, ?),", n), ",")
	if _, err := im.tx.Exec(query, im.rows...); err != nil {
		return err
	}
	im.rows = im.rows[:0]

	if im.inTx += n; im.inTx >= importBatch {
		return im.commit()
	}
	return nil
}

func (im *Importer) commit() error {
	if im.tx == nil {
		return nil
	}
	err := im.tx.Commit()
	im.tx, im.inTx = nil, 0
	return err
}

const createIndex = `CREATE UNIQUE INDEX IF NOT EXISTS domains_domain ON domains (domain)`

// Close saves the domains, removes duplicates and builds the index
func (im *Importer) Close() error {
	if err := errors.Join(im.insert(), im.commit()); err != nil {
		return err
	}

	if _, err := im.s.db.Exec(createIndex); err != nil {
		if !strings.Contains(err.Error(), "UNIQUE") {
			return err
		}
		if err := im.removeDuplicates(); err != nil {
			return err
		}
	}

	im.Added = im.Count() - im.Duplicates
	return nil
}

// removeDuplicates keeps the first rank of each domain and numbers the ranks
// again without gaps, so a rank stays the position in the list
func (im *Importer) removeDuplicates() error {
	tx, err := im.s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	statements := []string{
		`CREATE TABLE domains_unique (rank INTEGER PRIMARY KEY, domain TEXT NOT NULL, source TEXT NOT NULL)`,
		`INSERT INTO domains_unique (rank, domain, source)
			SELECT ROW_NUMBER() OVER (ORDER BY rank), domain, source FROM domains
			WHERE rank IN (SELECT MIN(rank) FROM domains GROUP BY domain)`,
		`DROP TABLE domains`,
		`ALTER TABLE domains_unique RENAME TO domains`,
		createIndex,
	}
	for _, statement := range statements {
		if _, err := tx.Exec(statement); err != nil {
			return err
		}
	}

	var unique int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM domains`).Scan(&unique); err != nil {
		return err
	}
	im.Duplicates = im.Count() - unique
	return tx.Commit()
}
