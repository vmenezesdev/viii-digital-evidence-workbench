package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

// fakeBacalhau records submissions and stops and describes every job as
// running on test-wasi with one execution named after the job.
type fakeBacalhau struct {
	mu        sync.Mutex
	submitted []map[string]any
	stopped   []string
	state     map[string]string // job ID -> job state
}

func (f *fakeBacalhau) server(t *testing.T) *httptest.Server {
	t.Helper()
	f.state = map[string]string{}
	api := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/nodes"):
			w.Write([]byte(`{"Nodes":[{"Info":{"NodeID":"test-wasi"},"Membership":"APPROVED"}]}`))
		case r.Method == http.MethodPut:
			var body struct{ Job map[string]any }
			json.NewDecoder(r.Body).Decode(&body)
			f.submitted = append(f.submitted, body.Job)
			id := "j-" + strings.TrimPrefix(body.Job["Name"].(string), "viii-")
			f.state[id] = "Running"
			json.NewEncoder(w).Encode(map[string]string{"JobID": id})
		case r.Method == http.MethodDelete:
			id := filepath.Base(r.URL.Path)
			f.stopped = append(f.stopped, id)
			f.state[id] = "Stopped"
			w.Write([]byte(`{}`))
		default:
			id := filepath.Base(r.URL.Path)
			state, ok := f.state[id]
			if !ok {
				http.Error(w, "not found", 404)
				return
			}
			compute := 6
			if state != "Running" {
				compute = 11
			}
			json.NewEncoder(w).Encode(map[string]any{"Job": map[string]any{"ID": id, "Name": "viii-" + strings.TrimPrefix(id, "j-"), "State": map[string]string{"StateType": state}}, "Executions": map[string]any{"Items": []any{map[string]any{"ID": "e-" + strings.TrimPrefix(id, "j-"), "NodeID": "test-wasi", "ComputeState": map[string]int{"StateType": compute}}}}})
		}
	}))
	t.Cleanup(api.Close)
	return api
}

func chunkTestService(t *testing.T, api string) (*pdfService, *pdfDocument) {
	t.Helper()
	s := &workbench{root: t.TempDir()}
	original := []byte("%PDF-1.7 synthetic chunking original")
	id := digest(original)
	for _, dir := range []string{"originals", id} {
		if err := os.MkdirAll(filepath.Join(s.root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(s.root, "originals", id+".pdf"), original, 0600); err != nil {
		t.Fatal(err)
	}
	chunker := filepath.Join(s.root, "pdf-chunker.wasm")
	if err := os.WriteFile(chunker, []byte("synthetic chunker"), 0600); err != nil {
		t.Fatal(err)
	}
	worker := filepath.Join(s.root, "pdf-worker.wasm")
	if err := os.WriteFile(worker, []byte("synthetic worker"), 0600); err != nil {
		t.Fatal(err)
	}
	p := &pdfService{s: s, Jobs: 2, runtime: &pdfRuntime{Root: s.root, name: "test", verified: true, Client: wasijob.Client{Base: api}, Chunker: chunker, ChunkerSHA: digest([]byte("synthetic chunker")), Worker: worker, WorkerSHA: digest([]byte("synthetic worker"))}}
	s.pdf = p
	d := &pdfDocument{EvidenceID: id, SHA256: id, Engine: "wasi", DesiredEngine: "wasi", Phase: "extract", ChunkSize: 4, Authorized: map[string]bool{"test-wasi": true}}
	if err := p.save(d); err != nil {
		t.Fatal(err)
	}
	return p, d
}

// writeChunk writes chunk bytes and its sidecar into an execution output dir.
func writeChunk(t *testing.T, dir, evidence string, first, last, pages, size int, content string) pdfChunk {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	c := pdfChunk{Schema: "viii.pdf-chunk-sidecar.v1", Tool: "flpdf", ToolVersion: flpdfVersion, ExtractRoute: "lean", SourcePages: pages, SourceSHA256: evidence, ChunkSize: size, File: fmt.Sprintf("chunk-%06d-%06d.pdf", first, last), First: first, Last: last, Count: last - first + 1, SHA256: digest([]byte(content)), Bytes: int64(len(content))}
	for n := first; n <= last; n++ {
		c.Pages = append(c.Pages, chunkPage{Local: n - first + 1, Physical: n})
	}
	if err := os.WriteFile(filepath.Join(dir, c.File), []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(c)
	if err := os.WriteFile(filepath.Join(dir, strings.TrimSuffix(c.File, ".pdf")+".json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestPDFPreparationStoresChunksProgressivelyAndVerifiesManifest(t *testing.T) {
	fake := &fakeBacalhau{}
	api := fake.server(t)
	p, d := chunkTestService(t, api.URL)
	ctx := context.Background()
	if err := p.advance(ctx, d); err != nil {
		t.Fatal(err)
	}
	if d.Prepare == "" || d.Chunking != chunkingPreparing || len(fake.submitted) != 1 || len(d.Jobs) != 0 {
		t.Fatalf("preparation not submitted alone: %+v %d", d, len(fake.submitted))
	}
	params := fake.submitted[0]["Tasks"].([]any)[0].(map[string]any)["Engine"].(map[string]any)["Params"].(map[string]any)["Parameters"].([]any)
	if fmt.Sprint(params) != fmt.Sprintf("[/original.pdf --chunk-size 4 --out-dir /out --ack /control/accepted --source-sha256 %s]", d.SHA256) {
		t.Fatal("unexpected preparation arguments", params)
	}
	a, err := p.s.load(d.EvidenceID, d.Prepare)
	if err != nil {
		t.Fatal(err)
	}
	a.ExecutionID = "e-" + d.Prepare
	out, _ := p.runtime.outputs(ctx, "wasi", a)
	first := writeChunk(t, out, d.EvidenceID, 1, 4, 6, 4, "chunk one bytes")
	if err := p.advance(ctx, d); err != nil {
		t.Fatal(err)
	}
	// The first verified chunk is usable while preparation continues.
	if d.Pages != 6 || d.Chunking != chunkingPreparing || len(d.Jobs) != 1 || d.Jobs[0].First != 1 || d.Jobs[0].Last != 4 {
		t.Fatalf("first chunk not consumed progressively: %+v", d)
	}
	job, _ := p.s.load(d.EvidenceID, d.Jobs[0].Attempt)
	if job.ChunkFirst != 1 || job.ChunkLast != 4 || job.ChunkSHA256 != first.SHA256 || !strings.HasPrefix(job.ChunkFile, p.chunkDir(d.EvidenceID)) {
		t.Fatalf("chunk provenance missing: %+v", job)
	}
	writeChunk(t, out, d.EvidenceID, 5, 6, 6, 4, "chunk two bytes")
	manifest := map[string]any{"schema": "viii.pdf-chunk-set.v1", "tool": "flpdf", "tool_version": flpdfVersion, "source_page_count": 6, "source_sha256_declared": d.SHA256, "physical_first": 1, "physical_last": 6, "chunk_size": 4, "chunks": []any{
		map[string]any{"file": "chunk-000001-000004.pdf", "physical_first": 1, "physical_last": 4, "chunk_sha256": digest([]byte("chunk one bytes"))},
		map[string]any{"file": "chunk-000005-000006.pdf", "physical_first": 5, "physical_last": 6, "chunk_sha256": digest([]byte("chunk two bytes"))}}, "diagnostics": []string{"repair: xref reconstructed"}}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(out, "manifest.json"), data, 0600)
	os.WriteFile(filepath.Join(out, "ready"), []byte("ready"), 0600)
	if err := p.advance(ctx, d); err != nil {
		t.Fatal(err)
	}
	if d.Chunking != chunkingReady || !strings.Contains(d.ChunkDiagnostic, "xref reconstructed") || len(d.Jobs) != 2 {
		t.Fatalf("chunk set not accepted or second chunk not scheduled: %+v", d)
	}
	if _, err := os.Stat(filepath.Join(a.StagingDir, "accepted")); err != nil {
		t.Fatal("preparation not acknowledged after verification", err)
	}
	stored, err := p.chunks(d)
	if err != nil || len(stored) != 2 || stored[0].PrepareAttempt != a.Attempt || stored[0].ChunkerSHA256 != p.runtime.ChunkerSHA || stored[0].EvidenceID != d.EvidenceID {
		t.Fatal("chunk records lack provenance", stored, err)
	}
	// The ready set's job is still reconciled to a terminal, recorded state.
	fake.mu.Lock()
	fake.state["j-"+a.Attempt] = "Completed"
	fake.mu.Unlock()
	if err := p.advance(ctx, d); err != nil {
		t.Fatal(err)
	}
	finished, _ := p.s.load(d.EvidenceID, a.Attempt)
	if d.Prepare != "" || finished.Finished == nil || finished.Status != "completed" {
		t.Fatalf("preparation history not finished: prepare=%q %+v", d.Prepare, finished)
	}
}

func TestPDFPreparationRejectsTamperedOrForeignChunks(t *testing.T) {
	for name, mutate := range map[string]func(dir string, c *pdfChunk){
		"bytes":    func(dir string, c *pdfChunk) { os.WriteFile(filepath.Join(dir, c.File), []byte("tampered"), 0600) },
		"evidence": func(dir string, c *pdfChunk) { c.SourceSHA256 = strings.Repeat("c", 64) },
		"map":      func(dir string, c *pdfChunk) { c.Pages[1].Physical = 9 },
		"plan":     func(dir string, c *pdfChunk) { c.First, c.File = 2, "chunk-000002-000004.pdf" },
	} {
		t.Run(name, func(t *testing.T) {
			fake := &fakeBacalhau{}
			api := fake.server(t)
			p, d := chunkTestService(t, api.URL)
			ctx := context.Background()
			if err := p.advance(ctx, d); err != nil {
				t.Fatal(err)
			}
			a, _ := p.s.load(d.EvidenceID, d.Prepare)
			a.ExecutionID = "e-" + d.Prepare
			out, _ := p.runtime.outputs(ctx, "wasi", a)
			c := writeChunk(t, out, d.EvidenceID, 1, 4, 6, 4, "chunk one bytes")
			mutate(out, &c)
			if name != "bytes" {
				data, _ := json.Marshal(c)
				os.WriteFile(filepath.Join(out, "chunk-000001-000004.json"), data, 0600)
			}
			if err := p.advance(ctx, d); err == nil {
				t.Fatal("accepted an unverifiable chunk")
			}
			if chunks, _ := p.chunks(d); len(chunks) != 0 || len(d.Jobs) != 0 {
				t.Fatal("unverified chunk became usable", chunks, d.Jobs)
			}
		})
	}
}

func TestPDFChunkJobMapsLocalPagesToPhysicalPages(t *testing.T) {
	fake := &fakeBacalhau{}
	api := fake.server(t)
	p, d := chunkTestService(t, api.URL)
	ctx := context.Background()
	dir := p.chunkDir(d.EvidenceID)
	c := writeChunk(t, dir, d.EvidenceID, 5, 8, 8, 4, "late chunk")
	c.EvidenceID = d.EvidenceID
	data, _ := json.Marshal(c)
	os.WriteFile(filepath.Join(dir, "chunk-000005-000008.json"), data, 0600)
	d.Pages, d.Chunking, d.Phase = 8, chunkingPreparing, "ocr"
	r := &pdfRange{First: 6, Last: 8, Skip: []int{7}}
	if err := p.submitRange(ctx, d, r, &c); err != nil {
		t.Fatal(err)
	}
	params := fake.submitted[0]["Tasks"].([]any)[0].(map[string]any)["Engine"].(map[string]any)["Params"].(map[string]any)["Parameters"].([]any)
	if params[1] != "2" || params[2] != "4" {
		t.Fatal("chunk job not given local pages", params)
	}
	a, _ := p.s.load(d.EvidenceID, r.Attempt)
	skip, _ := os.ReadFile(filepath.Join(a.StagingDir, "skip.json"))
	if string(skip) != "[3]" {
		t.Fatal("skip list not mapped to local pages", string(skip))
	}
	a.ExecutionID = "e-1"
	out, _ := p.runtime.outputs(ctx, "wasi", a)
	os.MkdirAll(out, 0700)
	for name, body := range map[string]string{
		"info.json":        `{"pages":4,"first":2,"last":4}`,
		"page-000002.json": `{"page":2,"text":"digital text on physical six","source":"digital","status":"complete","tool":"hayro","version":"0.7.1"}`,
		"page-000004.json": `{"page":4,"text":"JABUTICABA recognised on physical eight","source":"ocr","status":"complete","tool":"tesseract","version":"5"}`,
		"ready":            "ready",
	} {
		os.WriteFile(filepath.Join(out, name), []byte(body), 0600)
	}
	if err := p.ingestRange(ctx, d, a, r); err != nil {
		t.Fatal(err)
	}
	hits, err := p.s.searchPDF(d.EvidenceID, "JABUTICABA")
	if err != nil || len(hits) != 1 || hits[0].Page != 8 || !strings.HasSuffix(hits[0].URL, "#page=8") {
		t.Fatal("citation not on the original's physical page", hits, err)
	}
	if d.Pages != 8 {
		t.Fatal("chunk metadata overwrote the document page count", d.Pages)
	}
	if _, err := os.Stat(filepath.Join(a.StagingDir, "accepted")); err != nil {
		t.Fatal(err)
	}
	// A worker page that does not match its local position is rejected.
	os.WriteFile(filepath.Join(out, "page-000003.json"), []byte(`{"page":7,"text":"wrong","source":"digital","status":"complete","tool":"hayro","version":"0.7.1"}`), 0600)
	if err := p.ingestRange(ctx, d, a, &pdfRange{Attempt: r.Attempt, First: 6, Last: 8}); err == nil || !strings.Contains(err.Error(), "page reference") {
		t.Fatal("accepted a mismatched local page", err)
	}
}

func TestPDFPauseCancelsPreparationAndPageJobsUntilTerminal(t *testing.T) {
	fake := &fakeBacalhau{}
	api := fake.server(t)
	p, d := chunkTestService(t, api.URL)
	ctx := context.Background()
	p.advance(ctx, d)
	a, _ := p.s.load(d.EvidenceID, d.Prepare)
	a.ExecutionID = "e-" + d.Prepare
	out, _ := p.runtime.outputs(ctx, "wasi", a)
	writeChunk(t, out, d.EvidenceID, 1, 4, 8, 4, "first")
	if err := p.advance(ctx, d); err != nil || len(d.Jobs) != 1 {
		t.Fatal("expected one chunk job during preparation", err, d.Jobs)
	}
	p.save(d)
	w := httptest.NewRecorder()
	p.s.ServeHTTP(w, httptest.NewRequest("POST", "/api/pdf/pause", nil))
	if w.Code != 202 {
		t.Fatal(w.Code)
	}
	d, _ = p.load()
	if err := p.advance(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(fake.stopped) != 2 {
		t.Fatal("pause did not cancel the preparation and the page job", fake.stopped)
	}
	p.save(d)
	w = httptest.NewRecorder()
	p.s.ServeHTTP(w, httptest.NewRequest("POST", "/api/pdf/resume", nil))
	if w.Code != 409 {
		t.Fatal("resumed before cancellations were terminal", w.Code)
	}
	if err := p.advance(ctx, d); err != nil {
		t.Fatal(err)
	}
	if len(d.Jobs) != 0 || d.Prepare != "" || len(fake.submitted) != 2 {
		t.Fatalf("terminal jobs not resolved or new work submitted while paused: %+v %d", d, len(fake.submitted))
	}
	p.save(d)
	w = httptest.NewRecorder()
	p.s.ServeHTTP(w, httptest.NewRequest("POST", "/api/pdf/resume", nil))
	if w.Code != 202 {
		t.Fatal(w.Code, w.Body.String())
	}
	d, _ = p.load()
	if err := p.advance(ctx, d); err != nil {
		t.Fatal(err)
	}
	// Preparation resumes after the verified chunk; the stopped page job is redone.
	prepare := fake.submitted[2]["Tasks"].([]any)[0].(map[string]any)["Engine"].(map[string]any)["Params"].(map[string]any)["Parameters"].([]any)
	if fmt.Sprint(prepare[len(prepare)-2:]) != "[--range 5-8]" || len(d.Jobs) != 1 || d.Jobs[0].First != 1 {
		t.Fatal("preparation or page work not resumed from the verified state", prepare, d.Jobs)
	}
}

func TestPDFPreparationFailureFallsBackVisiblyToOneInPlaceJob(t *testing.T) {
	fake := &fakeBacalhau{}
	api := fake.server(t)
	p, d := chunkTestService(t, api.URL)
	ctx := context.Background()
	for i := 0; i < maxPrepareFailures; i++ {
		if err := p.advance(ctx, d); err != nil {
			t.Fatal(err)
		}
		fake.mu.Lock()
		fake.state["j-"+d.Prepare] = "Failed"
		fake.mu.Unlock()
		if err := p.advance(ctx, d); err != nil {
			t.Fatal(err)
		}
	}
	if d.Chunking != chunkingUnavailable || !strings.Contains(d.ChunkDiagnostic, "indisponível") || p.concurrency(d) != 1 {
		t.Fatalf("failed preparation not reported: %+v", d)
	}
	if len(d.Jobs) != 1 || d.Jobs[0].First != 1 || d.Jobs[0].Last != 1 {
		t.Fatalf("in-place fallback not scheduled: %+v", d.Jobs)
	}
	a, _ := p.s.load(d.EvidenceID, d.Jobs[0].Attempt)
	if a.ChunkFile != "" || a.Mode != "pdf-wasi" {
		t.Fatal("fallback job is not in place", a)
	}
}

func TestPDFLegacySingleAttemptStateStillLoads(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	p := &pdfService{s: s}
	id, attempt := strings.Repeat("a", 64), strings.Repeat("b", 26)
	legacy := fmt.Sprintf(`{"evidence_id":"%s","sha256":"%s","pages":30000,"engine":"wasi","desired_engine":"wasi","phase":"ocr","attempt":"%s","first":4510,"last":4637,"skip":[4511],"authorized_nodes":{"n":true},"paused":true,"complete":false}`, id, id, attempt)
	if err := os.WriteFile(filepath.Join(s.root, "pdf-document.json"), []byte(legacy), 0600); err != nil {
		t.Fatal(err)
	}
	d, err := p.load()
	if err != nil || len(d.Jobs) != 1 || d.Jobs[0].Attempt != attempt || d.Jobs[0].First != 4510 || d.Jobs[0].Skip[0] != 4511 {
		t.Fatal("legacy attempt not migrated", d, err)
	}
	if err := p.save(d); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(s.root, "pdf-document.json"))
	var mirrored map[string]any
	json.Unmarshal(data, &mirrored)
	if mirrored["attempt"] != attempt || mirrored["first"].(float64) != 4510 {
		t.Fatal("older readers lost the active attempt", string(data))
	}
	_ = time.Now
}
