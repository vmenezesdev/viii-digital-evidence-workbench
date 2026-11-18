package main

import (
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

func (s *workbench) pdfIndex(evidence string) (*sql.DB, error) {
	if _, err := hex.DecodeString(evidence); err != nil || len(evidence) != 64 {
		return nil, errors.New("invalid evidence digest")
	}
	dir := filepath.Join(s.root, evidence)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", filepath.Join(dir, "pages.sqlite"))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	_, err = db.Exec(`PRAGMA busy_timeout=5000;
CREATE TABLE IF NOT EXISTS pages(page INTEGER PRIMARY KEY, text TEXT NOT NULL, source TEXT NOT NULL,
 status TEXT NOT NULL, tool TEXT NOT NULL, version TEXT NOT NULL, engine TEXT NOT NULL, run_id TEXT NOT NULL);
CREATE VIRTUAL TABLE IF NOT EXISTS page_search USING fts5(text, content='pages', content_rowid='page', tokenize='unicode61 remove_diacritics 2');
CREATE TRIGGER IF NOT EXISTS pages_insert AFTER INSERT ON pages BEGIN
 INSERT INTO page_search(rowid,text) VALUES(new.page,new.text); END;`)
	if err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

func (s *workbench) indexPDFPage(evidence string, page pdfPage) error {
	db, err := s.pdfIndex(evidence)
	if err != nil {
		return err
	}
	defer db.Close()
	_, err = db.Exec(`INSERT OR IGNORE INTO pages VALUES(?,?,?,?,?,?,?,?)`,
		page.Page, page.Text, page.Source, page.Status, page.Tool, page.Version, page.Engine, page.RunID)
	return err
}

type pdfHit struct {
	Page int    `json:"page"`
	Text string `json:"text"`
	URL  string `json:"url"`
}

func (s *workbench) searchPDF(evidence, query string) ([]pdfHit, error) {
	hits := []pdfHit{}
	terms := strings.Fields(query)
	if len(terms) == 0 {
		return hits, nil
	}
	if len(terms) > 32 || len(query) > 2000 {
		return nil, errors.New("search query too long")
	}
	for i, term := range terms {
		terms[i] = `"` + strings.ReplaceAll(term, `"`, `""`) + `"`
	}
	db, err := s.pdfIndex(evidence)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query(`SELECT p.page,snippet(page_search,0,'','',' ... ',100) FROM page_search f JOIN pages p ON p.page=f.rowid
 WHERE page_search MATCH ? AND p.status='complete' ORDER BY rank LIMIT 10`, strings.Join(terms, " AND "))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var hit pdfHit
		if err := rows.Scan(&hit.Page, &hit.Text); err != nil {
			return nil, err
		}
		hit.URL = fmt.Sprintf("/api/pdf/original?evidence=%s#page=%d", evidence, hit.Page)
		hits = append(hits, hit)
	}
	return hits, rows.Err()
}
