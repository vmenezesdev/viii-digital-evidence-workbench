package main

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestImportPersistenceAndRetry(t *testing.T) {
	b, e := os.ReadFile("../../docs/research/fixtures/ufdr/messages-minimal.xml")
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	for i := 0; i < 2; i++ {
		// Reconstruct the service each time to exercise on-disk evidence identity recovery.
		s := &workbench{root: root}
		w := httptest.NewRecorder()
		s.ServeHTTP(w, httptest.NewRequest("POST", "/api/process?mode=native", bytes.NewReader(b)))
		var record map[string]any
		if w.Code != 200 {
			t.Fatal(w.Body.String())
		}
		if e = json.Unmarshal(w.Body.Bytes(), &record); e != nil {
			t.Fatal(e)
		}
		if record["error"] != nil {
			t.Fatal(record["error"])
		}
		if record["evidence_id"] != digest(b) {
			t.Fatal("unstable identity")
		}
		saved, e := os.ReadFile(filepath.Join(root, digest(b), "original.xml"))
		if e != nil || !bytes.Equal(saved, b) {
			t.Fatal("original bytes changed", e)
		}
	}
	dirs, _ := os.ReadDir(root)
	if len(dirs) != 1 {
		t.Fatal("duplicate evidence")
	}
	entries, _ := filepath.Glob(filepath.Join(root, digest(b), "*.json"))
	if len(entries) != 2 {
		t.Fatalf("expected two historical attempts, got %d", len(entries))
	}
}
