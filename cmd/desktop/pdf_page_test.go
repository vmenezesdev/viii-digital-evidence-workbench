package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPDFPageImmutableAcrossEngineSwitch(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	id := strings.Repeat("a", 64)
	page := pdfPage{Page: 29, Text: "JABUTICABA", Source: "ocr", Status: "complete", Tool: "tesseract", Version: "5.5.0", Engine: "wasi", RunID: "execution-1"}
	if _, err := s.savePDFPage(id, page); err != nil {
		t.Fatal(err)
	}
	page.Engine, page.RunID, page.Text = "docker", "execution-2", "replacement"
	reopened := &workbench{root: s.root}
	got, err := reopened.savePDFPage(id, page)
	if err != nil || got.Engine != "wasi" || got.RunID != "execution-1" || got.Text != "JABUTICABA" || got.Page != 29 {
		t.Fatalf("accepted result changed on reopen/engine switch: %+v %v", got, err)
	}
	for _, invalid := range []pdfPage{{Page: 0}, {Page: 30001}, {Page: 29, Status: "complete"}} {
		if _, err := s.savePDFPage(id, invalid); err == nil {
			t.Fatalf("invalid worker result accepted: %+v", invalid)
		}
	}
	if _, err := s.savePDFPage("../outside", page); err == nil {
		t.Fatal("unsafe evidence path accepted")
	}
}

func TestPDFSearchPartialAndPhysicalCitation(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	id := strings.Repeat("a", 64)
	page := pdfPage{Page: 3, Text: "Contrato: obrigação de pagamento", Source: "digital", Status: "complete", Tool: "hayro", Version: "0.7.1", Engine: "wasi", RunID: "execution-1"}
	if _, err := s.savePDFPage(id, page); err != nil {
		t.Fatal(err)
	}
	hits, err := s.searchPDF(id, "obrigacao")
	if err != nil || len(hits) != 1 || hits[0].Page != 3 || !strings.HasSuffix(hits[0].URL, "#page=3") {
		t.Fatalf("partial search/citation failed: %+v %v", hits, err)
	}
	for _, query := range []string{"JABUTICABA", `" OR 1=1 --`, "NEAR(foo)"} {
		if hits, err := s.searchPDF(id, query); err != nil || len(hits) != 0 {
			t.Fatalf("literal query %q: %+v %v", query, hits, err)
		}
	}
}

func TestPDFMixedArtifacts(t *testing.T) {
	dir := os.Getenv("VIII_PDF_PAGES_PROOF")
	if dir == "" {
		t.Skip("set VIII_PDF_PAGES_PROOF to a successful 12-page OCR proof")
	}
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		Passed      bool
		Execution   string
		Engine      string
		InputSHA256 string `json:"input_sha256"`
	}
	if err := json.Unmarshal(data, &summary); err != nil || !summary.Passed {
		t.Fatal("no successful runtime proof", err)
	}
	s := &workbench{root: filepath.Join(dir, "viii")}
	id := strings.ToLower(summary.InputSHA256)
	if summary.Engine == "" {
		summary.Engine = "wasi"
	}
	for number := 1; number <= 12; number++ {
		data, err := os.ReadFile(filepath.Join(dir, "artifacts", fmt.Sprintf("page-%06d.json", number)))
		if err != nil {
			t.Fatal(err)
		}
		var page pdfPage
		if err := json.Unmarshal(data, &page); err != nil {
			t.Fatal(err)
		}
		if page.Page != number {
			t.Fatal("physical page changed", page.Page, number)
		}
		if number == 8 && page.Status != "unreadable" {
			t.Fatal("blank page not accounted for")
		}
		page.Engine, page.RunID = summary.Engine, summary.Execution
		if _, err := s.savePDFPage(id, page); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := s.searchPDF(id, "JABUTICABA")
	if err != nil || len(hits) != 1 || hits[0].Page != 11 || !strings.Contains(hits[0].Text, "15 de março") || !strings.HasSuffix(hits[0].URL, "#page=11") {
		t.Fatalf("scanned-page search/citation failed: %+v %v", hits, err)
	}
}

// Opt-in real-artifact check, run by prove-pdf-ocr.ps1 after Bacalhau exits.
func TestPDFOCRArtifact(t *testing.T) {
	dir := os.Getenv("VIII_PDF_OCR_PROOF")
	if dir == "" {
		t.Skip("run scripts/prove-pdf-ocr.ps1 for the Windows WASI gate")
	}
	var summary struct {
		Passed      bool
		Execution   string
		InputSHA256 string `json:"input_sha256"`
	}
	read := func(name string, result any) {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, result); err != nil {
			t.Fatal(err)
		}
	}
	read("summary.json", &summary)
	if !summary.Passed || summary.Execution == "" {
		t.Fatal("missing verified runtime identity")
	}
	var page pdfPage
	read("page-1.json", &page)
	page.Engine, page.RunID = "wasi", summary.Execution
	s := &workbench{root: filepath.Join(dir, "viii")}
	got, err := s.savePDFPage(strings.ToLower(summary.InputSHA256), page)
	if err != nil || !strings.Contains(got.Text, "JABUTICABA") || !strings.Contains(got.Text, "obrigação") {
		t.Fatalf("real Portuguese OCR artifact did not persist: %+v %v", got, err)
	}
}
