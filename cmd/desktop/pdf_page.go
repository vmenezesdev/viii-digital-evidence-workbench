package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// pdfPage is shared by the WASI and Docker workers; Page is the physical PDF page.
type pdfPage struct {
	Page    int    `json:"page"`
	Text    string `json:"text"`
	Source  string `json:"source"`
	Status  string `json:"status"`
	Tool    string `json:"tool"`
	Version string `json:"version"`
	Engine  string `json:"engine"`
	RunID   string `json:"run_id"`
	Error   string `json:"error,omitempty"`
}

// Accepted pages are immutable, including across retries and engine changes.
func (s *workbench) savePDFPage(evidence string, page pdfPage) (pdfPage, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := hex.DecodeString(evidence); err != nil || len(evidence) != 64 {
		return pdfPage{}, errors.New("invalid evidence digest")
	}
	if page.Page < 1 || page.Page > 30000 || len(page.Text) > 2<<20 ||
		(page.Engine != "wasi" && page.Engine != "docker") || page.RunID == "" || page.Tool == "" || page.Version == "" ||
		(page.Source != "digital" && page.Source != "ocr") ||
		(page.Status != "complete" && page.Status != "error" && page.Status != "unreadable") ||
		(page.Status == "complete" && strings.TrimSpace(page.Text) == "") {
		return pdfPage{}, errors.New("invalid page result or provenance")
	}
	dir := filepath.Join(s.root, evidence, "pages")
	path := filepath.Join(dir, fmt.Sprintf("%06d.json", page.Page))
	previous, err := os.ReadFile(path)
	if err == nil {
		var accepted pdfPage
		err = json.Unmarshal(previous, &accepted)
		if err == nil && accepted.Page != page.Page {
			err = errors.New("persisted page identity mismatch")
		}
		if err == nil {
			err = s.indexPDFPage(evidence, accepted)
		}
		return accepted, err
	}
	if !os.IsNotExist(err) {
		return pdfPage{}, err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return pdfPage{}, err
	}
	data, err := json.Marshal(page)
	if err == nil {
		err = atomicWrite(path, data)
	}
	if err == nil {
		err = s.indexPDFPage(evidence, page)
	}
	return page, err
}
