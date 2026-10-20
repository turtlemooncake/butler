package store

import (
	"database/sql"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
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
); CREATE TABLE IF NOT EXISTS chunks (
	id        INTEGER PRIMARY KEY,
	page_id   INTEGER NOT NULL,
	seq       INTEGER NOT NULL,
	text      TEXT NOT NULL,
	embedding BLOB NOT NULL,
	UNIQUE (page_id, seq)
); CREATE TABLE IF NOT EXISTS sites (
	url TEXT PRIMARY KEY
)
`

const insertSchema = `
INSERT INTO pages (url, title, text, fetched_at) VALUES (?, ?, ?, ?)
ON CONFLICT (url) DO UPDATE SET
	title = excluded.title,
	text = excluded.text,
	fetched_at = excluded.fetched_at`

const allChunksQuery = `SELECT pages.url, pages.title, chunks.seq, chunks.text, chunks.embedding
FROM chunks
JOIN pages ON pages.id = chunks.page_id
ORDER BY chunks.id
`

type Store struct {
	db *sql.DB
}

type Page struct {
	URL       string
	Title     string
	Text      string
	FetchedAt time.Time
}

type Chunk struct {
	Text      string
	Embedding []float32
}

// IndexedChunk = chunk + page of chunk
type IndexedChunk struct {
	URL       string
	Title     string
	Seq       int
	Text      string
	Embedding []float32
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
	// If the page's text changed, its chunks are out of date: delete them so
	// the next index embeds the page again.
	_, err := s.db.Exec(`
		DELETE FROM chunks WHERE page_id =
			(SELECT id FROM pages WHERE url = ? AND text <> ?)`,
		p.URL, p.Text)
	if err != nil {
		return fmt.Errorf("saving %s: %w", p.URL, err)
	}

	_, err = s.db.Exec(insertSchema, p.URL, p.Title, p.Text, p.FetchedAt.UTC().Format(time.RFC3339))
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

// ListPages returns every stored page, oldest first.
func (s *Store) ListPages() ([]Page, error) {
	return s.queryPages(`SELECT url, title, text, fetched_at FROM pages ORDER BY id`)
}

func (s *Store) PagesToIndex() ([]Page, error) {
	return s.queryPages(`
		SELECT url, title, text, fetched_at FROM pages
		WHERE id NOT IN (SELECT page_id FROM chunks)
		ORDER BY id`)
}

// queryPages runs query, which must select url, title, text, fetched_at,
// and returns the pages.
func (s *Store) queryPages(query string) ([]Page, error) {
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var p Page
		var fetchedAt string

		err := rows.Scan(&p.URL, &p.Title, &p.Text, &fetchedAt)
		if err != nil {
			return nil, err
		}

		p.FetchedAt, err = time.Parse(time.RFC3339, fetchedAt)
		if err != nil {
			return nil, err
		}

		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return pages, nil
}

// SaveChunks replaces all chunks of the page at url. It returns ErrNotFound
// if there's no such page.
func (s *Store) SaveChunks(url string, chunks []Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var pageID int64
	err = tx.QueryRow(`SELECT id FROM pages WHERE url = ?`, url).Scan(&pageID)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("saving chunks for %s: %w", url, err)
	}

	if _, err := tx.Exec(`DELETE FROM chunks WHERE page_id = ?`, pageID); err != nil {
		return fmt.Errorf("saving chunks for %s: %w", url, err)
	}

	for i, c := range chunks {
		_, err := tx.Exec(`INSERT INTO chunks (page_id, seq, text, embedding) VALUES (?, ?, ?, ?)`,
			pageID, i, c.Text, encodeVector(c.Embedding))
		if err != nil {
			return fmt.Errorf("saving chunks for %s: %w", url, err)
		}
	}

	return tx.Commit()
}

func (s *Store) CountChunks() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM chunks`).Scan(&n)
	return n, err
}

func (s *Store) DeleteAllChunks() error {
	_, err := s.db.Exec(`DELETE FROM chunks`)
	return err
}

// encodeVector packs v into 4 little-endian bytes per float.
func encodeVector(v []float32) []byte {
	b := make([]byte, 0, len(v)*4)
	for _, f := range v {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(f))
	}
	return b
}

// decodeVector unpacks bytes written by encodeVector.
func decodeVector(b []byte) ([]float32, error) {
	if len(b)%4 != 0 {
		return nil, fmt.Errorf("cannot be vector, cannot be divided by 4")
	}

	v := make([]float32, len(b)/4)

	for i := range v {
		v[i] = math.Float32frombits(binary.LittleEndian.Uint32(b[i*4 : i*4+4]))
	}

	return v, nil
}

func (s *Store) AllChunks() ([]IndexedChunk, error) {
	rows, err := s.db.Query(allChunksQuery)
	if err != nil {
		return nil, err
	}

	defer rows.Close()

	var chunks []IndexedChunk
	for rows.Next() {
		var ic IndexedChunk
		var e []byte

		err := rows.Scan(&ic.URL, &ic.Title, &ic.Seq, &ic.Text, &e)
		if err != nil {
			return nil, err
		}

		ic.Embedding, err = decodeVector(e)
		if err != nil {
			return nil, fmt.Errorf(`chunk decode yielded error. chunk: %s seq: %d. %w`, ic.URL, ic.Seq, err)
		}

		chunks = append(chunks, ic)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return chunks, nil
}

// SaveSite remembers url as a crawl start, so refresh can crawl it again.
func (s *Store) SaveSite(url string) error {
	_, err := s.db.Exec(`INSERT INTO sites (url) VALUES (?) ON CONFLICT DO NOTHING`, url)
	return err
}

// ListSites returns every saved crawl start. alphabetical order.
func (s *Store) ListSites() ([]string, error) {
	rows, err := s.db.Query(`SELECT url FROM sites ORDER BY url`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sites []string
	for rows.Next() {
		var u string
		if err := rows.Scan(&u); err != nil {
			return nil, err
		}
		sites = append(sites, u)
	}
	return sites, rows.Err()
}
