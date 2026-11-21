package main

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

// pdfRange is one active page job: physical pages [First, Last] minus Skip.
type pdfRange struct {
	Attempt string `json:"attempt"`
	First   int    `json:"first"`
	Last    int    `json:"last"`
	Skip    []int  `json:"skip,omitempty"`
}

type pdfDocument struct {
	EvidenceID    string `json:"evidence_id"`
	SHA256        string `json:"sha256"`
	Name          string `json:"name"`
	Bytes         int64  `json:"bytes"`
	Pages         int    `json:"pages"`
	Engine        string `json:"engine"`
	DesiredEngine string `json:"desired_engine"`
	Phase         string `json:"phase"`
	// Attempt, First, Last and Skip mirror Jobs[0] for older readers.
	Attempt    string          `json:"attempt,omitempty"`
	First      int             `json:"first"`
	Last       int             `json:"last"`
	Skip       []int           `json:"skip,omitempty"`
	Jobs       []pdfRange      `json:"jobs,omitempty"`
	Authorized map[string]bool `json:"authorized_nodes"`
	Paused     bool            `json:"paused"`
	// Cancel asks every active job (pages and preparation) to stop; set by pause.
	Cancel     bool   `json:"cancel_requested,omitempty"`
	Complete   bool   `json:"complete"`
	Diagnostic string `json:"diagnostic,omitempty"`

	Chunking        string `json:"chunking,omitempty"`
	ChunkSize       int    `json:"chunk_size,omitempty"`
	Prepare         string `json:"prepare,omitempty"`
	PrepareFailures int    `json:"prepare_failures,omitempty"`
	ChunkDiagnostic string `json:"chunk_diagnostic,omitempty"`
}

// normalize moves a single legacy attempt into Jobs.
func (d *pdfDocument) normalize() {
	if len(d.Jobs) == 0 && d.Attempt != "" {
		d.Jobs = []pdfRange{{Attempt: d.Attempt, First: d.First, Last: d.Last, Skip: d.Skip}}
	}
}

func (d *pdfDocument) mirror() {
	d.Attempt, d.Skip = "", nil
	if len(d.Jobs) > 0 {
		d.Attempt, d.First, d.Last, d.Skip = d.Jobs[0].Attempt, d.Jobs[0].First, d.Jobs[0].Last, d.Jobs[0].Skip
	}
}

type pdfService struct {
	s       *workbench
	runtime *pdfRuntime
	// Jobs is the maximum number of simultaneous page jobs on chunks.
	Jobs    int
	mu      sync.Mutex
	stateMu sync.RWMutex
}

func fileDigest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (p *pdfService) load() (*pdfDocument, error) {
	p.stateMu.RLock()
	defer p.stateMu.RUnlock()
	data, err := os.ReadFile(filepath.Join(p.s.root, "pdf-document.json"))
	if err != nil {
		return nil, err
	}
	var d pdfDocument
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	if _, err := hex.DecodeString(d.EvidenceID); err != nil || len(d.EvidenceID) != 64 || d.SHA256 != d.EvidenceID {
		return nil, errors.New("invalid PDF identity")
	}
	d.normalize()
	valid := (d.Engine == "wasi" || d.Engine == "docker") && (d.DesiredEngine == "wasi" || d.DesiredEngine == "docker") && (d.Phase == "extract" || d.Phase == "ocr") && d.Pages >= 0 && d.Pages <= 30000 && d.Authorized != nil && (d.Prepare == "" || validAttempt(d.Prepare)) && d.ChunkSize >= 0 && d.ChunkSize <= 1000
	switch d.Chunking {
	case "", chunkingPreparing, chunkingReady, chunkingUnavailable:
	default:
		valid = false
	}
	for _, r := range d.Jobs {
		if !validAttempt(r.Attempt) || r.First < 1 || r.Last < r.First {
			valid = false
		}
	}
	if !valid {
		return nil, errors.New("invalid persisted PDF state")
	}
	return &d, nil
}

func (p *pdfService) save(d *pdfDocument) error {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
	d.normalize()
	d.mirror()
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(filepath.Join(p.s.root, "pdf-document.json"), data)
}

func (p *pdfService) original(id string) string {
	return filepath.Join(p.s.root, "originals", id+".pdf")
}

func (s *workbench) servePDF(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if s.pdf == nil {
		http.Error(w, "PDF runtime is not configured", http.StatusServiceUnavailable)
		return
	}
	if r.Header.Get("Sec-Fetch-Site") == "cross-site" {
		http.Error(w, "Cross-site request denied", http.StatusForbidden)
		return
	}
	p := s.pdf
	if r.URL.Path == "/api/pdf/import" && r.Method == http.MethodPost {
		p.mu.Lock()
		defer p.mu.Unlock()
		if err := p.importPDF(r); err != nil {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
		w.WriteHeader(http.StatusAccepted)
		return
	}
	d, err := p.load()
	if os.IsNotExist(err) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "null")
		return
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	switch {
	case r.URL.Path == "/api/pdf/status" && r.Method == http.MethodGet:
		db, err := s.pdfIndex(d.EvidenceID)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		defer db.Close()
		var searchable, failed, accepted, ocr int
		err = db.QueryRow(`SELECT COUNT(*),COALESCE(SUM(status='complete'),0),COALESCE(SUM(status!='complete'),0),COALESCE(SUM(source='ocr'),0) FROM pages`).Scan(&accepted, &searchable, &failed, &ocr)
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		failures := []map[string]any{}
		rows, err := db.Query("SELECT page,status FROM pages WHERE status!='complete' ORDER BY page LIMIT 100")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		for rows.Next() {
			var page int
			var status string
			if err := rows.Scan(&page, &status); err != nil {
				rows.Close()
				http.Error(w, err.Error(), 500)
				return
			}
			failures = append(failures, map[string]any{"page": page, "status": status, "url": fmt.Sprintf("/api/pdf/original?evidence=%s#page=%d", d.EvidenceID, page)})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		chunks, _ := p.chunks(d)
		prepared := 0
		for _, c := range chunks {
			prepared += c.Count
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"document": d, "searchable": searchable, "failed": failed, "pending": max(0, d.Pages-accepted), "ocr_processed": ocr, "failures": failures, "active_jobs": len(d.Jobs), "chunks": len(chunks), "chunked_pages": prepared})
	case r.URL.Path == "/api/pdf/engine" && r.Method == http.MethodPost:
		p.mu.Lock()
		defer p.mu.Unlock()
		d, err = p.load()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		engine := r.URL.Query().Get("engine")
		if engine != "wasi" && engine != "docker" {
			http.Error(w, "Invalid engine", 400)
			return
		}
		if engine == "docker" {
			ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
			defer cancel()
			if err := p.runtime.ensureDocker(ctx); err != nil {
				http.Error(w, "Habilite o Docker Desktop (Linux containers) e carregue a imagem do worker. O caminho WASI continua disponível. "+err.Error(), 503)
				return
			}
		}
		d.DesiredEngine = engine
		if d.Paused && !d.Cancel && p.dropFinished(d) {
			d.Paused = false
		}
		d.Authorized[p.runtime.node(engine)] = true
		if err := p.save(d); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(202)
	case r.URL.Path == "/api/pdf/pause" && r.Method == http.MethodPost:
		p.mu.Lock()
		defer p.mu.Unlock()
		d, err = p.load()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		d.Paused, d.Cancel = true, true
		if err := p.save(d); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(202)
	case r.URL.Path == "/api/pdf/resume" && r.Method == http.MethodPost:
		p.mu.Lock()
		defer p.mu.Unlock()
		d, err = p.load()
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if !p.dropFinished(d) || d.Prepare != "" {
			http.Error(w, "Reconcile the current job before retrying", 409)
			return
		}
		d.Paused, d.Cancel = false, false
		d.Diagnostic = ""
		if err := p.save(d); err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.WriteHeader(202)
	case r.URL.Path == "/api/pdf/search" && r.Method == http.MethodGet:
		hits, err := s.searchPDF(d.EvidenceID, r.URL.Query().Get("q"))
		if err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(hits)
	case r.URL.Path == "/api/pdf/ask" && r.Method == http.MethodPost:
		var question struct {
			Query    string `json:"query"`
			Question string `json:"question"`
		}
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8192)).Decode(&question); err != nil {
			http.Error(w, "Invalid question", 400)
			return
		}
		answer, err := s.answerPDF(r.Context(), d.EvidenceID, question.Query, question.Question, p.runtime.Inference, d.Pages)
		if err != nil {
			http.Error(w, "Inicie o llama-server local configurado para perguntas. "+err.Error(), 503)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(answer)
	case r.URL.Path == "/api/pdf/original" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		if r.URL.Query().Get("evidence") != d.EvidenceID {
			http.NotFound(w, r)
			return
		}
		path := p.original(d.EvidenceID)
		actual, err := fileDigest(path)
		if err != nil || actual != d.SHA256 {
			http.Error(w, "Original PDF integrity failure", 500)
			return
		}
		w.Header().Set("Content-Type", "application/pdf")
		w.Header().Set("Content-Disposition", "inline; filename=original.pdf")
		http.ServeFile(w, r, path)
	default:
		http.Error(w, "Method not allowed", 405)
	}
}

// dropFinished removes jobs whose attempt is already finished and reports
// whether no unfinished job remains.
func (p *pdfService) dropFinished(d *pdfDocument) bool {
	var keep []pdfRange
	for _, r := range d.Jobs {
		if a, err := p.s.load(d.EvidenceID, r.Attempt); err != nil || a.Finished == nil {
			keep = append(keep, r)
		}
	}
	d.Jobs = keep
	return len(keep) == 0
}

func (p *pdfService) importPDF(r *http.Request) error {
	if d, err := p.load(); err == nil && !d.Complete {
		return errors.New("Finalize o PDF atual antes de importar outro")
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}
	dir := filepath.Join(p.s.root, "originals")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".import-*.pdf")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	header := make([]byte, 5)
	if _, err := io.ReadFull(r.Body, header); err != nil || string(header) != "%PDF-" {
		return errors.New("Selecione um PDF válido")
	}
	hash := sha256.New()
	writer := io.MultiWriter(f, hash)
	if _, err := writer.Write(header); err != nil {
		return err
	}
	n, err := io.Copy(writer, io.LimitReader(r.Body, 8<<30))
	if err != nil {
		return err
	}
	if n >= 8<<30 {
		return errors.New("PDF exceeds the import byte limit")
	}
	if err := errors.Join(f.Sync(), f.Close()); err != nil {
		return err
	}
	id := hex.EncodeToString(hash.Sum(nil))
	path := p.original(id)
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := os.Rename(f.Name(), path); err != nil {
			return err
		}
	} else {
		actual, err := fileDigest(path)
		if err != nil || actual != id {
			return errors.New("Original PDF integrity failure")
		}
	}
	if err := os.MkdirAll(filepath.Join(p.s.root, id), 0700); err != nil {
		return err
	}
	if previous, err := p.load(); err == nil && previous.EvidenceID == id {
		return nil
	}
	d := &pdfDocument{EvidenceID: id, SHA256: id, Name: filepath.Base(r.URL.Query().Get("name")), Bytes: n + 5, Engine: "wasi", DesiredEngine: "wasi", Phase: "extract", Authorized: map[string]bool{p.runtime.node("wasi"): true}, ChunkSize: defaultChunkSize}
	return p.save(d)
}

func (p *pdfService) run(ctx context.Context) {
	ticker := time.NewTicker(750 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !p.mu.TryLock() {
				continue
			}
			func() {
				defer p.mu.Unlock()
				d, err := p.load()
				// A paused document still reconciles its active jobs until they are terminal.
				if err != nil || d.Complete || (d.Paused && len(d.Jobs) == 0 && d.Prepare == "") {
					return
				}
				step, cancel := context.WithTimeout(ctx, 2*time.Minute)
				defer cancel()
				if err := p.advance(step, d); err != nil {
					d.Diagnostic = err.Error()
				}
				if err := p.save(d); err != nil {
					fmt.Fprintln(os.Stderr, "PDF state persistence failed:", err)
				}
			}()
		}
	}
}

func terminal(description *wasijob.Description, a *attemptRecord) bool {
	done := description.Job.State.StateType == "Completed" || description.Job.State.StateType == "Failed" || description.Job.State.StateType == "Stopped" || description.Job.State.StateType == "Cancelled" || description.Job.State.StateType == "Canceled"
	for _, x := range a.Executions {
		if x.ComputeState.StateType != 9 && x.ComputeState.StateType != 10 && x.ComputeState.StateType != 11 {
			done = false
		}
	}
	return done
}

// describe reconciles an attempt with Bacalhau. A persisted submission that
// never reached the orchestrator is replayed after re-checking the placement
// grant and every mounted input; it then returns a nil description.
func (p *pdfService) describe(ctx context.Context, d *pdfDocument, a *attemptRecord) (*wasijob.Description, error) {
	key := a.JobID
	if key == "" {
		key = a.jobName()
	}
	description, err := p.runtime.Client.Describe(ctx, key)
	if err != nil {
		if a.JobID == "" && wasijob.IsNotFound(err) {
			if !d.Authorized[a.RequestedNodeID] {
				return nil, errors.New("Evidence placement denied")
			}
			if a.InputSHA256 != d.SHA256 {
				return nil, errors.New("Original PDF integrity failure")
			}
			if a.ChunkFile == "" {
				actual, err := fileDigest(p.original(d.EvidenceID))
				if err != nil || actual != d.SHA256 {
					return nil, errors.New("Original PDF integrity failure")
				}
			} else if actual, err := fileDigest(a.ChunkFile); err != nil || actual != a.ChunkSHA256 {
				return nil, errors.New("Chunk integrity failure")
			}
			worker := map[string]string{"pdf-wasi": p.runtime.Worker, "pdf-chunk-wasi": p.runtime.Chunker}[a.Mode]
			if worker != "" {
				actual, err := fileDigest(worker)
				if err != nil || actual != a.WorkerSHA256 {
					return nil, errors.New("Worker integrity failure")
				}
			}
			data, readErr := os.ReadFile(filepath.Join(a.StagingDir, "submission.json"))
			if readErr != nil {
				return nil, readErr
			}
			var spec map[string]any
			if err := json.Unmarshal(data, &spec); err != nil {
				return nil, err
			}
			if spec["Name"] != a.jobName() {
				return nil, errors.New("Submission identity mismatch")
			}
			// v1.9.5 rejects an unchanged named spec instead of executing it again.
			a.JobID, err = p.runtime.Client.Submit(ctx, spec)
			if err == nil {
				a.Status = "accepted"
			}
			if saveErr := p.s.save(a); saveErr != nil {
				return nil, saveErr
			}
			return nil, err
		}
		return nil, fmt.Errorf("Reconciliação pendente: %w", err)
	}
	if description.Job.Name != a.jobName() || description.Job.ID == "" || (a.JobID != "" && description.Job.ID != a.JobID) {
		return nil, errors.New("Job identity mismatch")
	}
	a.JobID = description.Job.ID
	a.JobState = description.Job.State
	a.JobCreatedNS = description.Job.CreateTime
	a.Executions = nil
	for _, x := range description.Executions.Items {
		if x.NodeID != a.RequestedNodeID {
			return nil, errors.New("Unauthorized execution node")
		}
		a.Executions = append(a.Executions, executionRecord{ID: x.ID, NodeID: x.NodeID, ComputeState: x.ComputeState, DesiredState: x.DesiredState, CreateTime: x.CreateTime, ModifyTime: x.ModifyTime})
	}
	if len(a.Executions) > 1 {
		return nil, errors.New("Multiple executions require review before accepting results")
	}
	if len(a.Executions) == 1 {
		a.ExecutionID = a.Executions[0].ID
		a.NodeID = a.Executions[0].NodeID
	}
	return &description, nil
}

// reconcile advances one page job and reports whether it is finished.
func (p *pdfService) reconcile(ctx context.Context, d *pdfDocument, r *pdfRange) (bool, error) {
	a, err := p.s.load(d.EvidenceID, r.Attempt)
	if err != nil {
		return false, err
	}
	if a.Finished != nil {
		return true, nil
	}
	description, err := p.describe(ctx, d, a)
	if err != nil || description == nil {
		return false, err
	}
	if len(a.Executions) == 1 {
		if err := p.ingestRange(ctx, d, a, r); err != nil {
			return false, err
		}
	}
	if (d.DesiredEngine != d.Engine || d.Cancel) && a.CancelRequested == nil {
		now := time.Now().UTC()
		a.CancelRequested = &now
		if err := p.s.save(a); err != nil {
			return false, err
		}
	}
	if !terminal(description, a) {
		if a.CancelRequested != nil {
			if err := p.runtime.Client.Stop(ctx, a.JobID); err != nil {
				return false, err
			}
			d.Diagnostic = "Aguardando encerramento dos jobs em andamento"
		}
		return false, p.s.save(a)
	}
	if description.Job.State.StateType == "Completed" {
		if _, err := description.Result(a.RequestedNodeID); err != nil {
			a.finish("failed", err)
			d.Paused = true
			d.Diagnostic = "Não foi possível concluir o lote. Os resultados confirmados foram preservados; use Retomar para tentar as páginas restantes."
		} else {
			a.finish("completed", nil)
		}
	} else if a.CancelRequested != nil {
		a.finish("stopped", nil)
	} else {
		a.finish("failed", errors.New("Bacalhau job failed"))
		d.Paused = true
		d.Diagnostic = "O lote falhou. Os resultados confirmados foram preservados; use Retomar para tentar as páginas restantes."
	}
	return true, p.s.save(a)
}

func (p *pdfService) concurrency(d *pdfDocument) int {
	// In place, every job holds the whole original: keep one.
	if d.Chunking == chunkingUnavailable || p.Jobs < 1 {
		return 1
	}
	return p.Jobs
}

func (p *pdfService) advance(ctx context.Context, d *pdfDocument) error {
	d.normalize()
	defer d.mirror()
	if err := p.runtime.ensure(ctx); err != nil {
		return err
	}
	d.Diagnostic = ""
	if err := p.advancePrepare(ctx, d); err != nil {
		return err
	}
	var active []pdfRange
	for i := range d.Jobs {
		done, err := p.reconcile(ctx, d, &d.Jobs[i])
		if err != nil {
			return err
		}
		if !done {
			active = append(active, d.Jobs[i])
		}
	}
	d.Jobs = active
	if d.Paused || len(d.Jobs) > 0 && d.Engine != d.DesiredEngine {
		return nil
	}
	if d.Engine != d.DesiredEngine {
		d.Engine = d.DesiredEngine
	}
	if d.Engine == "docker" {
		if err := p.runtime.ensureDocker(ctx); err != nil {
			return err
		}
	}
	visited, err := p.visited(d)
	if err != nil {
		return err
	}
	for _, r := range d.Jobs {
		for n := r.First; n <= r.Last; n++ {
			visited[n] = true
		}
	}
	if d.Chunking != chunkingUnavailable {
		chunks, err := p.chunks(d)
		if err != nil {
			return err
		}
		for len(d.Jobs) < p.concurrency(d) {
			r, c := p.nextRange(d, visited, chunks)
			if r == nil {
				break
			}
			if err := p.submitRange(ctx, d, r, c); err != nil {
				return err
			}
			for n := r.First; n <= r.Last; n++ {
				visited[n] = true
			}
		}
		if len(d.Jobs) > 0 || d.Chunking != chunkingReady {
			return nil
		}
	} else if len(d.Jobs) > 0 {
		return nil
	}
	first, last, skip := p.pendingFrom(d, visited)
	if first == 0 {
		if d.Phase == "extract" {
			d.Phase = "ocr"
			return nil
		}
		if d.Prepare != "" {
			return nil // the preparation job's history must be terminal first
		}
		d.Complete = true
		d.Diagnostic = ""
		return nil
	}
	if d.Chunking != chunkingUnavailable {
		return errors.New("Chunk set does not cover the pending pages")
	}
	return p.submitRange(ctx, d, &pdfRange{First: first, Last: last, Skip: skip}, nil)
}

// visited reports the physical pages already handled in the current phase.
func (p *pdfService) visited(d *pdfDocument) (map[int]bool, error) {
	visited := map[int]bool{}
	db, err := p.s.pdfIndex(d.EvidenceID)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT page FROM pages")
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return nil, err
		}
		visited[n] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	// A crash can leave a durable page file just before its SQLite commit.
	entries, err := os.ReadDir(filepath.Join(p.s.root, d.EvidenceID, "pages"))
	if err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || n < 1 || (d.Pages != 0 && n > d.Pages) || entry.Name() != fmt.Sprintf("%06d.json", n) {
			return nil, errors.New("Invalid persisted page filename")
		}
		if visited[n] {
			continue
		}
		data, err := readPDFArtifact(filepath.Join(p.s.root, d.EvidenceID, "pages", entry.Name()))
		if err != nil {
			return nil, err
		}
		var page pdfPage
		if err := json.Unmarshal(data, &page); err != nil {
			return nil, err
		}
		if page.Page != n {
			return nil, errors.New("Persisted page identity mismatch")
		}
		if _, err := p.s.savePDFPage(d.EvidenceID, page); err != nil {
			return nil, err
		}
		visited[n] = true
	}
	if d.Phase == "extract" {
		entries, err := os.ReadDir(filepath.Join(p.s.root, d.EvidenceID, "digital"))
		if err != nil && !os.IsNotExist(err) {
			return nil, err
		}
		for _, entry := range entries {
			n, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
			if err == nil {
				visited[n] = true
			}
		}
	}
	return visited, nil
}

func (p *pdfService) pending(d *pdfDocument) (int, int, []int, error) {
	visited, err := p.visited(d)
	if err != nil {
		return 0, 0, nil, err
	}
	first, last, skip := p.pendingFrom(d, visited)
	return first, last, skip, nil
}

// pendingFrom returns the next in-place range of up to 128 pages.
func (p *pdfService) pendingFrom(d *pdfDocument, visited map[int]bool) (int, int, []int) {
	if d.Pages == 0 {
		return 1, 1, nil
	}
	for first := 1; first <= d.Pages; first++ {
		if visited[first] {
			continue
		}
		last := min(d.Pages, first+127)
		skip := []int{}
		for n := first; n <= last; n++ {
			if visited[n] {
				skip = append(skip, n)
			}
		}
		return first, last, skip
	}
	return 0, 0, nil
}

// submit starts an in-place job on the original.
func (p *pdfService) submit(ctx context.Context, d *pdfDocument, first, last int, skip []int) error {
	d.normalize()
	defer d.mirror()
	return p.submitRange(ctx, d, &pdfRange{First: first, Last: last, Skip: skip}, nil)
}

// submitRange starts a page job on the original (c == nil) or on one verified
// chunk. Chunk jobs see local pages 1..n; the attempt records the map.
func (p *pdfService) submitRange(ctx context.Context, d *pdfDocument, r *pdfRange, c *pdfChunk) error {
	id := strings.ToLower(rand.Text())
	node := p.runtime.node(d.Engine)
	worker, workerHash := p.runtime.Worker, p.runtime.WorkerSHA
	if d.Engine == "docker" {
		worker = p.runtime.Image
		workerHash = p.runtime.ImageID
	}
	policy := &wasijob.Placement{Allow: map[string]map[string]bool{d.EvidenceID: d.Authorized}, Modules: map[string]string{node: worker}, WorkerSHA256: workerHash, Client: p.runtime.Client}
	if !policy.Authorize(d.EvidenceID, node) {
		return errors.New("Evidence placement denied")
	}
	input, offset := p.original(d.EvidenceID), 0
	if c == nil {
		actual, err := fileDigest(input)
		if err != nil || actual != d.SHA256 {
			return errors.New("Original PDF integrity failure")
		}
	} else {
		input, offset = filepath.Join(p.chunkDir(d.EvidenceID), c.File), c.First-1
		if actual, err := fileDigest(input); err != nil || actual != c.SHA256 {
			return errors.New("Chunk integrity failure")
		}
	}
	control := filepath.Join(p.s.root, "control", id)
	if err := os.MkdirAll(control, 0700); err != nil {
		return err
	}
	if r.Skip == nil {
		r.Skip = []int{}
	}
	local := make([]int, 0, len(r.Skip))
	for _, n := range r.Skip {
		local = append(local, n-offset)
	}
	data, err := json.Marshal(local)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(control, "skip.json"), data); err != nil {
		return err
	}
	a := &attemptRecord{EvidenceID: d.EvidenceID, InputSHA256: d.SHA256, Attempt: id, RequestID: id, Mode: "pdf-" + d.Engine, Location: "local-only", Status: "submitting", Started: time.Now().UTC(), WorkerVersion: "pdf-v1", WorkerSHA256: workerHash, WorkerURL: worker, Configuration: fmt.Sprintf("%s:%d-%d", d.Phase, r.First, r.Last), Authorization: "allow", RequestedNodeID: node, Orchestrator: p.runtime.Client.Base, StagingDir: control}
	if c != nil {
		a.ChunkFile, a.ChunkSHA256, a.ChunkFirst, a.ChunkLast = input, c.SHA256, c.First, c.Last
	}
	spec, err := p.runtime.spec(ctx, input, control, id, node, d.Phase, r.First-offset, r.Last-offset, c != nil)
	if err != nil {
		return err
	}
	data, err = json.Marshal(spec)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(control, "submission.json"), data); err != nil {
		return err
	}
	if err := p.s.save(a); err != nil {
		return err
	}
	r.Attempt = id
	d.Jobs = append(d.Jobs, *r)
	d.Diagnostic = ""
	if err := p.save(d); err != nil {
		return err
	}
	a.JobID, err = p.runtime.Client.Submit(ctx, spec)
	if err != nil {
		a.Diagnostic = "Submission outcome unknown: " + err.Error()
	} else {
		a.Status = "accepted"
	}
	if saveErr := p.s.save(a); saveErr != nil {
		return saveErr
	}
	return err
}

func readPDFArtifact(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 3<<20+1))
	if len(data) > 3<<20 {
		return nil, errors.New("Page artifact exceeds size limit")
	}
	return data, err
}

// ingest accepts the results of the document's job for attempt a.
func (p *pdfService) ingest(ctx context.Context, d *pdfDocument, a *attemptRecord) error {
	d.normalize()
	defer d.mirror()
	for i := range d.Jobs {
		if d.Jobs[i].Attempt == a.Attempt {
			return p.ingestRange(ctx, d, a, &d.Jobs[i])
		}
	}
	return errors.New("Attempt is not active for this document")
}

func (p *pdfService) ingestRange(ctx context.Context, d *pdfDocument, a *attemptRecord, r *pdfRange) error {
	engine := "wasi"
	if a.Mode == "pdf-docker" {
		engine = "docker"
	}
	out, err := p.runtime.outputs(ctx, engine, a)
	if err != nil {
		return err
	}
	data, err := readPDFArtifact(filepath.Join(out, "info.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var info struct{ Pages, First, Last int }
	if err := json.Unmarshal(data, &info); err != nil {
		return nil
	}
	offset := 0
	if a.ChunkFirst > 0 {
		offset = a.ChunkFirst - 1
		if a.ChunkLast < a.ChunkFirst || r.First < a.ChunkFirst || r.Last > a.ChunkLast || info.Pages != a.ChunkLast-a.ChunkFirst+1 || info.First != r.First-offset || info.Last != r.Last-offset {
			return errors.New("Worker returned invalid chunk/range metadata")
		}
	} else {
		if info.Pages < 1 || info.Pages > 30000 || info.First != r.First || info.Last != r.Last || (d.Pages != 0 && info.Pages != d.Pages) {
			return errors.New("Worker returned invalid PDF/range metadata")
		}
		d.Pages = info.Pages
	}
	db, err := p.s.pdfIndex(d.EvidenceID)
	if err != nil {
		return err
	}
	indexed := map[int]bool{}
	rows, err := db.Query("SELECT page FROM pages WHERE page BETWEEN ? AND ?", r.First, r.Last)
	if err != nil {
		db.Close()
		return err
	}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			db.Close()
			return err
		}
		indexed[n] = true
	}
	err = rows.Err()
	rows.Close()
	db.Close()
	if err != nil {
		return err
	}
	skipped := map[int]bool{}
	for _, n := range r.Skip {
		skipped[n] = true
	}
	ready := true
	for n := r.First; n <= r.Last; n++ {
		if skipped[n] || indexed[n] {
			continue
		}
		if d.Phase == "extract" {
			if _, err := os.Stat(filepath.Join(p.s.root, d.EvidenceID, "digital", fmt.Sprintf("%06d.json", n))); err == nil {
				continue
			}
		}
		local := n - offset
		data, err := readPDFArtifact(filepath.Join(out, fmt.Sprintf("page-%06d.json", local)))
		if os.IsNotExist(err) {
			ready = false
			continue
		}
		if err != nil {
			return err
		}
		var page pdfPage
		if err := json.Unmarshal(data, &page); err != nil {
			ready = false
			continue
		}
		if page.Page != local {
			return errors.New("Worker page reference mismatch")
		}
		// Only the verified chunk map turns a local page into a physical one.
		page.Page = n
		if d.Phase == "extract" && page.Status == "needs_ocr" {
			if page.Source != "digital" || page.Tool != "hayro" || page.Version == "" {
				return errors.New("Invalid digital page observation")
			}
		} else {
			page.Engine = engine
			page.RunID = a.ExecutionID
			if _, err := p.s.savePDFPage(d.EvidenceID, page); err != nil {
				return err
			}
		}
		if d.Phase == "extract" {
			dir := filepath.Join(p.s.root, d.EvidenceID, "digital")
			if err := os.MkdirAll(dir, 0700); err != nil {
				return err
			}
			path := filepath.Join(dir, fmt.Sprintf("%06d.json", n))
			if _, err := os.Stat(path); os.IsNotExist(err) {
				observation, err := json.Marshal(page)
				if err != nil {
					return err
				}
				if err := atomicWrite(path, observation); err != nil {
					return err
				}
			}
		}
	}
	if ready {
		if _, err := os.Stat(filepath.Join(out, "ready")); err == nil {
			// noop removes artifacts on exit, so metadata must survive before ACK.
			if err := p.save(d); err != nil {
				return err
			}
			return p.runtime.acknowledge(ctx, engine, a)
		}
	}
	return nil
}
