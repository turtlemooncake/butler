package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

const schema = `
CREATE TABLE IF NOT EXISTS pages (
	id         INTEGER PRIMARY KEY,
	url        TEXT NOT NULL UNIQUE,
	title      TEXT NOT NULL,
	text       TEXT NOT NULL,
	fetched_at TEXT NOT NULL
)`

const insertSchema = `
INSERT INTO pages (url, title, text, fetched_at) VALUES (?, ?, ?, ?)
ON CONFLICT (url) DO UPDATE SET
	title = excluded.title,
	text = excluded.text,
	fetched_at = excluded.fetched_at`

type Store struct {
	db *sql.DB
}

type Page struct {
	URL       string
	Title     string
	Text      string
	FetchedAt time.Time
}

var ErrNotFound = errors.New("page not found")

func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating tables in %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// s *Store is equivalent to python's self
func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) SavePage(p Page) error {
	_, err := s.db.Exec(insertSchema, p.URL, p.Title, p.Text, p.FetchedAt.UTC().Format(time.RFC3339))
	if err != nil {
		return fmt.Errorf("saving %s: %w", p.URL, err)
	}

	return nil
}

func (s *Store) CountPages() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pages`).Scan(&n)
	return n, err
}

func (s *Store) GetPage(url string) (Page, error) {
	var p Page
	var fetchedAt string
	err := s.db.QueryRow(`SELECT url, title, text, fetched_at FROM pages WHERE url = ?`, url).
		Scan(&p.URL, &p.Title, &p.Text, &fetchedAt)

	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, ErrNotFound
	}

	if err != nil {
		return Page{}, err
	}

	p.FetchedAt, err = time.Parse(time.RFC3339, fetchedAt)
	if err != nil {
		return Page{}, err
	}

	return p, nil
}
