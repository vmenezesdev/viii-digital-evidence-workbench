package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

func TestPDFImportAndOriginalIntegrity(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	s.pdf = &pdfService{s: s, runtime: &pdfRuntime{Root: s.root}}
	input := []byte("%PDF-1.4\nsynthetic import boundary fixture\n%%EOF")
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/pdf/import?name=small.pdf", bytes.NewReader(input)))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	d, err := s.pdf.load()
	if err != nil {
		t.Fatal(err)
	}
	if d.SHA256 != digest(input) || !d.Authorized[s.pdf.runtime.node("wasi")] {
		t.Fatal("missing hash or explicit local placement grant")
	}
	r := httptest.NewRequest("GET", "/api/pdf/original?evidence="+d.EvidenceID, nil)
	r.Header.Set("Range", "bytes=0-4")
	w = httptest.NewRecorder()
	s.ServeHTTP(w, r)
	if w.Code != 206 || w.Body.String() != "%PDF-" {
		t.Fatal("original PDF range retrieval", w.Code, w.Body.String())
	}
	if err := os.WriteFile(s.pdf.original(d.EvidenceID), []byte("changed"), 0600); err != nil {
		t.Fatal(err)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", "/api/pdf/original?evidence="+d.EvidenceID, nil))
	if w.Code != 500 {
		t.Fatal("mutated original was served")
	}
}

func TestPDFPendingSkipsConfirmedAndDigitalObservations(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	p := &pdfService{s: s}
	id := strings.Repeat("a", 64)
	for _, n := range []int{1, 4} {
		if _, err := s.savePDFPage(id, pdfPage{Page: n, Text: "confirmed", Source: "digital", Status: "complete", Tool: "hayro", Version: "0.7.1", Engine: "wasi", RunID: "e-1"}); err != nil {
			t.Fatal(err)
		}
	}
	dir := filepath.Join(s.root, id, "digital")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "000002.json"), []byte(`{"page":2,"status":"needs_ocr"}`)); err != nil {
		t.Fatal(err)
	}
	d := &pdfDocument{EvidenceID: id, Pages: 6, Phase: "extract"}
	first, last, skip, err := p.pending(d)
	if err != nil || first != 3 || last != 6 || !reflect.DeepEqual(skip, []int{4}) {
		t.Fatal(first, last, skip, err)
	}
	d.Phase = "ocr"
	first, last, skip, err = p.pending(d)
	if err != nil || first != 2 || last != 6 || !reflect.DeepEqual(skip, []int{4}) {
		t.Fatal(first, last, skip, err)
	}
}

func TestPDFReopenReconcilesAcceptedJobBeforeSwitch(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	id := strings.Repeat("a", 64)
	attempt := strings.Repeat("b", 26)
	puts, stops := 0, 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			json.NewEncoder(w).Encode(map[string]any{"Nodes": []any{map[string]any{"Info": map[string]string{"NodeID": "test-wasi"}, "Membership": "APPROVED"}}})
		case r.Method == http.MethodDelete:
			stops++
			w.Write([]byte(`{}`))
		case r.Method == http.MethodPut:
			puts++
			w.Write([]byte(`{"JobID":"unexpected"}`))
		default:
			json.NewEncoder(w).Encode(map[string]any{"Job": map[string]any{"ID": "j-accepted", "Name": "viii-" + attempt, "State": map[string]string{"StateType": "Running"}}, "Executions": map[string]any{"Items": []any{map[string]any{"ID": "e-live", "NodeID": "test-wasi", "ComputeState": map[string]int{"StateType": 6}}}}})
		}
	}))
	defer api.Close()
	if err := os.MkdirAll(filepath.Join(s.root, id), 0700); err != nil {
		t.Fatal(err)
	}
	a := &attemptRecord{EvidenceID: id, InputSHA256: id, Attempt: attempt, RequestID: attempt, Started: time.Now(), RequestedNodeID: "test-wasi", Mode: "pdf-wasi", Status: "submitting"}
	if err := s.save(a); err != nil {
		t.Fatal(err)
	}
	d := &pdfDocument{EvidenceID: id, SHA256: id, Pages: 1, Engine: "wasi", DesiredEngine: "docker", Phase: "ocr", Attempt: attempt, First: 1, Last: 1}
	// A new service instance sees the accepted job by its persisted name, even
	// though the original submission response never reached the application.
	reopened := &workbench{root: s.root}
	p := &pdfService{s: reopened, runtime: &pdfRuntime{Root: s.root, name: "test", verified: true, Client: wasijob.Client{Base: api.URL}}}
	if err := p.advance(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	stored, err := reopened.load(id, attempt)
	if err != nil {
		t.Fatal(err)
	}
	if stored.JobID != "j-accepted" || stored.ExecutionID != "e-live" || stored.CancelRequested == nil || puts != 0 || stops != 1 || d.Engine != "wasi" || d.Attempt != attempt {
		t.Fatalf("premature reassignment or duplicate submission: %+v puts=%d stops=%d doc=%+v", stored, puts, stops, d)
	}
}

func TestPDFPlacementDeniedBeforeStaging(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	p := &pdfService{s: s, runtime: &pdfRuntime{Root: s.root, Worker: "worker", WorkerSHA: "hash", Client: wasijob.Client{Base: "http://127.0.0.1:1"}}}
	d := &pdfDocument{EvidenceID: strings.Repeat("a", 64), Engine: "wasi", Authorized: map[string]bool{}}
	if err := p.submit(context.Background(), d, 1, 1, nil); err == nil || !strings.Contains(err.Error(), "placement denied") {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(s.root, "control")); !os.IsNotExist(err) {
		t.Fatal("unauthorized job was staged")
	}
}

func TestPDFUnavailableDockerKeepsWASI(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"Nodes": []any{map[string]any{"Info": map[string]string{"NodeID": "test-wasi"}, "Membership": "APPROVED"}}})
	}))
	defer api.Close()
	s := &workbench{root: t.TempDir()}
	p := &pdfService{s: s, runtime: &pdfRuntime{Root: s.root, name: "test", verified: true, Client: wasijob.Client{Base: api.URL}}}
	s.pdf = p
	d := &pdfDocument{EvidenceID: strings.Repeat("a", 64), SHA256: strings.Repeat("a", 64), Engine: "wasi", DesiredEngine: "wasi", Phase: "extract", Authorized: map[string]bool{"test-wasi": true}}
	if err := p.save(d); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/pdf/engine?engine=docker", nil))
	if w.Code != 503 || !strings.Contains(w.Body.String(), "WASI continua") {
		t.Fatal(w.Code, w.Body.String())
	}
	reopened, err := p.load()
	if err != nil || reopened.Engine != "wasi" || reopened.DesiredEngine != "wasi" || reopened.Paused {
		t.Fatal("Docker failure disabled WASI", reopened, err)
	}
}

func TestPDFReopenBeforeSubmissionRetriesPersistedSpec(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	input := []byte("%PDF-1.4 synthetic retry fixture")
	id, attempt := digest(input), strings.Repeat("b", 26)
	if err := os.MkdirAll(filepath.Join(s.root, id), 0700); err != nil {
		t.Fatal(err)
	}
	control := filepath.Join(s.root, "control", attempt)
	if err := os.MkdirAll(control, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.root, "originals"), 0700); err != nil {
		t.Fatal(err)
	}
	original := filepath.Join(s.root, "originals", id+".pdf")
	worker := filepath.Join(s.root, "worker.wasm")
	for path, data := range map[string][]byte{original: input, worker: []byte("synthetic worker")} {
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	spec := map[string]any{"Name": "viii-" + attempt, "Count": float64(1)}
	data, _ := json.Marshal(spec)
	if err := atomicWrite(filepath.Join(control, "submission.json"), data); err != nil {
		t.Fatal(err)
	}
	puts := 0
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			w.Write([]byte(`{"Nodes":[{"Info":{"NodeID":"test-wasi"},"Membership":"APPROVED"}]}`))
		case r.Method == "PUT":
			var body struct{ Job map[string]any }
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !reflect.DeepEqual(body.Job, spec) {
				t.Error("changed persisted submission", err, body)
			}
			puts++
			w.Write([]byte(`{"JobID":"j-recovered"}`))
		default:
			http.Error(w, "not found", 404)
		}
	}))
	defer api.Close()
	a := &attemptRecord{EvidenceID: id, InputSHA256: id, Attempt: attempt, RequestID: attempt, Started: time.Now(), RequestedNodeID: "test-wasi", Mode: "pdf-wasi", Status: "submitting", StagingDir: control, WorkerSHA256: digest([]byte("synthetic worker"))}
	if err := s.save(a); err != nil {
		t.Fatal(err)
	}
	p := &pdfService{s: s, runtime: &pdfRuntime{Root: s.root, name: "test", verified: true, Worker: worker, Client: wasijob.Client{Base: api.URL}}}
	d := &pdfDocument{EvidenceID: id, SHA256: id, Engine: "wasi", DesiredEngine: "wasi", Phase: "extract", Attempt: attempt, Authorized: map[string]bool{"test-wasi": true}}
	if err := p.advance(context.Background(), d); err != nil {
		t.Fatal(err)
	}
	stored, err := s.load(id, attempt)
	if err != nil || stored.JobID != "j-recovered" || puts != 1 || d.Attempt != attempt {
		t.Fatal(stored, err, puts)
	}
	stored.JobID = ""
	if err := s.save(stored); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{original, worker} {
		before, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("changed since persisted intent"), 0600); err != nil {
			t.Fatal(err)
		}
		if err := p.advance(context.Background(), d); err == nil || !strings.Contains(err.Error(), "integrity failure") || puts != 1 {
			t.Fatal("resubmitted changed input", path, err, puts)
		}
		if err := os.WriteFile(path, before, 0600); err != nil {
			t.Fatal(err)
		}
	}
	d.Authorized = map[string]bool{}
	if err := p.advance(context.Background(), d); err == nil || !strings.Contains(err.Error(), "placement denied") || puts != 1 {
		t.Fatal("resubmitted without placement grant", err, puts)
	}
}

func TestPDFMetadataDurableBeforeArtifactAcknowledgement(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	p := &pdfService{s: s, runtime: &pdfRuntime{Root: s.root}}
	id, attempt := strings.Repeat("a", 64), strings.Repeat("b", 26)
	a := &attemptRecord{Attempt: attempt, ExecutionID: "e-live", StagingDir: filepath.Join(s.root, "control", attempt)}
	d := &pdfDocument{EvidenceID: id, SHA256: id, Engine: "wasi", DesiredEngine: "wasi", Phase: "extract", Attempt: attempt, First: 1, Last: 1, Authorized: map[string]bool{}}
	if err := p.save(d); err != nil {
		t.Fatal(err)
	}
	out, _ := p.runtime.outputs(context.Background(), "wasi", a)
	for _, dir := range []string{out, a.StagingDir} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	for name, data := range map[string]string{"info.json": `{"pages":12,"first":1,"last":1}`, "page-000001.json": `{"page":1,"text":"persisted page text","source":"digital","status":"complete","tool":"hayro","version":"0.7.1"}`, "ready": "ready"} {
		if err := os.WriteFile(filepath.Join(out, name), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.ingest(context.Background(), d, a); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.StagingDir, "accepted")); err != nil {
		t.Fatal(err)
	}
	stored, err := p.load()
	if err != nil || stored.Pages != 12 || stored.Attempt != attempt {
		t.Fatal("ACK preceded durable metadata", stored, err)
	}
}

func TestPDFPendingRestoresDurablePageWithoutReprocessing(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			s := &workbench{root: t.TempDir()}
			p := &pdfService{s: s}
			id := strings.Repeat("a", 64)
			dir := filepath.Join(s.root, id, "pages")
			if err := os.MkdirAll(dir, 0700); err != nil {
				t.Fatal(err)
			}
			page := pdfPage{Page: 1, Text: "ARARA confirmed before index commit", Source: "digital", Status: "complete", Tool: "hayro", Version: "0.7.1", Engine: "wasi", RunID: "original-execution"}
			if mismatch {
				page.Page = 2
			}
			data, _ := json.Marshal(page)
			path := filepath.Join(dir, "000001.json")
			if err := os.WriteFile(path, data, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, ".viii-interrupted"), []byte("partial"), 0600); err != nil {
				t.Fatal(err)
			}
			first, _, _, err := p.pending(&pdfDocument{EvidenceID: id, Pages: 3, Phase: "ocr"})
			if mismatch {
				if err == nil {
					t.Fatal("accepted mismatched physical page")
				}
				return
			}
			if err != nil || first != 2 {
				t.Fatal("durable page scheduled again", first, err)
			}
			hits, err := s.searchPDF(id, "ARARA")
			if err != nil || len(hits) != 1 || hits[0].Page != 1 {
				t.Fatal(hits, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(data, after) {
				t.Fatal("recovery overwrote provenance", err)
			}
		})
	}
}
