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

type pdfDocument struct {
	EvidenceID    string          `json:"evidence_id"`
	SHA256        string          `json:"sha256"`
	Name          string          `json:"name"`
	Bytes         int64           `json:"bytes"`
	Pages         int             `json:"pages"`
	Engine        string          `json:"engine"`
	DesiredEngine string          `json:"desired_engine"`
	Phase         string          `json:"phase"`
	Attempt       string          `json:"attempt,omitempty"`
	First         int             `json:"first"`
	Last          int             `json:"last"`
	Skip          []int           `json:"skip,omitempty"`
	Authorized    map[string]bool `json:"authorized_nodes"`
	Paused        bool            `json:"paused"`
	Complete      bool            `json:"complete"`
	Diagnostic    string          `json:"diagnostic,omitempty"`
}

type pdfService struct {
	s       *workbench
	runtime *pdfRuntime
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
	if (d.Engine != "wasi" && d.Engine != "docker") || (d.DesiredEngine != "wasi" && d.DesiredEngine != "docker") || (d.Phase != "extract" && d.Phase != "ocr") || d.Pages < 0 || d.Pages > 30000 || d.Authorized == nil || (d.Attempt != "" && !validAttempt(d.Attempt)) {
		return nil, errors.New("invalid persisted PDF state")
	}
	return &d, nil
}

func (p *pdfService) save(d *pdfDocument) error {
	p.stateMu.Lock()
	defer p.stateMu.Unlock()
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"document": d, "searchable": searchable, "failed": failed, "pending": max(0, d.Pages-accepted), "ocr_processed": ocr, "failures": failures})
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
		if d.Paused {
			if d.Attempt == "" {
				d.Paused = false
			} else if a, e := s.load(d.EvidenceID, d.Attempt); e == nil && a.Finished != nil {
				d.Attempt = ""
				d.Paused = false
			}
		}
		d.Authorized[p.runtime.node(engine)] = true
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
		if d.Attempt != "" {
			a, e := s.load(d.EvidenceID, d.Attempt)
			if e != nil || a.Finished == nil {
				http.Error(w, "Reconcile the current job before retrying", 409)
				return
			}
		}
		d.Attempt = ""
		d.Paused = false
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
	d := &pdfDocument{EvidenceID: id, SHA256: id, Name: filepath.Base(r.URL.Query().Get("name")), Bytes: n + 5, Engine: "wasi", DesiredEngine: "wasi", Phase: "extract", Authorized: map[string]bool{p.runtime.node("wasi"): true}}
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
				if err != nil || d.Complete || d.Paused {
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

func (p *pdfService) advance(ctx context.Context, d *pdfDocument) error {
	if err := p.runtime.ensure(ctx); err != nil {
		return err
	}
	d.Diagnostic = ""
	if d.Attempt != "" {
		a, err := p.s.load(d.EvidenceID, d.Attempt)
		if err != nil {
			return err
		}
		key := a.JobID
		if key == "" {
			key = a.jobName()
		}
		description, err := p.runtime.Client.Describe(ctx, key)
		if err != nil {
			if a.JobID == "" && wasijob.IsNotFound(err) {
				if !d.Authorized[a.RequestedNodeID] {
					return errors.New("Evidence placement denied")
				}
				actual, err := fileDigest(p.original(d.EvidenceID))
				if err != nil || actual != d.SHA256 || a.InputSHA256 != d.SHA256 {
					return errors.New("Original PDF integrity failure")
				}
				if a.Mode == "pdf-wasi" {
					actual, err := fileDigest(p.runtime.Worker)
					if err != nil || actual != a.WorkerSHA256 {
						return errors.New("Worker integrity failure")
					}
				}
				data, readErr := os.ReadFile(filepath.Join(a.StagingDir, "submission.json"))
				if readErr != nil {
					return readErr
				}
				var spec map[string]any
				if err := json.Unmarshal(data, &spec); err != nil {
					return err
				}
				if spec["Name"] != a.jobName() {
					return errors.New("Submission identity mismatch")
				}
				// v1.9.5 rejects an unchanged named spec instead of executing it again.
				a.JobID, err = p.runtime.Client.Submit(ctx, spec)
				if err == nil {
					a.Status = "accepted"
				}
				if saveErr := p.s.save(a); saveErr != nil {
					return saveErr
				}
				return err
			}
			return fmt.Errorf("Reconciliação pendente: %w", err)
		}
		if description.Job.Name != a.jobName() || description.Job.ID == "" || (a.JobID != "" && description.Job.ID != a.JobID) {
			return errors.New("Job identity mismatch")
		}
		a.JobID = description.Job.ID
		a.JobState = description.Job.State
		a.JobCreatedNS = description.Job.CreateTime
		a.Executions = nil
		for _, x := range description.Executions.Items {
			if x.NodeID != a.RequestedNodeID {
				return errors.New("Unauthorized execution node")
			}
			a.Executions = append(a.Executions, executionRecord{ID: x.ID, NodeID: x.NodeID, ComputeState: x.ComputeState, DesiredState: x.DesiredState, CreateTime: x.CreateTime, ModifyTime: x.ModifyTime})
		}
		if len(a.Executions) > 1 {
			return errors.New("Multiple executions require review before accepting results")
		}
		if len(a.Executions) == 1 {
			a.ExecutionID = a.Executions[0].ID
			a.NodeID = a.Executions[0].NodeID
			if err := p.ingest(ctx, d, a); err != nil {
				return err
			}
		}
		if d.DesiredEngine != d.Engine && a.CancelRequested == nil {
			now := time.Now().UTC()
			a.CancelRequested = &now
			if err := p.s.save(a); err != nil {
				return err
			}
		}
		terminal := a.JobState.StateType == "Completed" || a.JobState.StateType == "Failed" || a.JobState.StateType == "Stopped" || a.JobState.StateType == "Cancelled" || a.JobState.StateType == "Canceled"
		for _, x := range a.Executions {
			if x.ComputeState.StateType != 9 && x.ComputeState.StateType != 10 && x.ComputeState.StateType != 11 {
				terminal = false
			}
		}
		if !terminal {
			if a.CancelRequested != nil {
				if err := p.runtime.Client.Stop(ctx, a.JobID); err != nil {
					return err
				}
				d.Diagnostic = "Aguardando encerramento do job antes de trocar o engine"
			}
			return p.s.save(a)
		}
		if a.JobState.StateType == "Completed" {
			if _, err := description.Result(a.RequestedNodeID); err != nil {
				a.finish("failed", err)
				d.Paused = true
				d.Diagnostic = "Não foi possível concluir o lote. Os resultados confirmados foram preservados; use Retomar para tentar as páginas restantes."
			} else {
				a.finish("completed", nil)
				d.Diagnostic = ""
			}
		} else if a.CancelRequested != nil {
			a.finish("stopped", nil)
		} else {
			a.finish("failed", errors.New("Bacalhau job failed"))
			d.Paused = true
			d.Diagnostic = "O lote falhou. Os resultados confirmados foram preservados; use Retomar para tentar as páginas restantes."
		}
		if err := p.s.save(a); err != nil {
			return err
		}
		d.Attempt = ""
		if d.Paused {
			return nil
		}
	}
	if d.Engine != d.DesiredEngine {
		d.Engine = d.DesiredEngine
	}
	if d.Engine == "docker" {
		if err := p.runtime.ensureDocker(ctx); err != nil {
			return err
		}
	}
	first, last, skip, err := p.pending(d)
	if err != nil {
		return err
	}
	if first == 0 {
		if d.Phase == "extract" {
			d.Phase = "ocr"
			return nil
		}
		d.Complete = true
		d.Diagnostic = ""
		return nil
	}
	return p.submit(ctx, d, first, last, skip)
}

func (p *pdfService) pending(d *pdfDocument) (int, int, []int, error) {
	if d.Pages == 0 {
		return 1, 1, nil, nil
	}
	visited := map[int]bool{}
	db, err := p.s.pdfIndex(d.EvidenceID)
	if err != nil {
		return 0, 0, nil, err
	}
	defer db.Close()
	rows, err := db.Query("SELECT page FROM pages")
	if err != nil {
		return 0, 0, nil, err
	}
	for rows.Next() {
		var n int
		if err := rows.Scan(&n); err != nil {
			rows.Close()
			return 0, 0, nil, err
		}
		visited[n] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return 0, 0, nil, err
	}
	// A crash can leave a durable page file just before its SQLite commit.
	entries, err := os.ReadDir(filepath.Join(p.s.root, d.EvidenceID, "pages"))
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
		if err != nil || n < 1 || n > d.Pages || entry.Name() != fmt.Sprintf("%06d.json", n) {
			return 0, 0, nil, errors.New("Invalid persisted page filename")
		}
		if visited[n] {
			continue
		}
		data, err := readPDFArtifact(filepath.Join(p.s.root, d.EvidenceID, "pages", entry.Name()))
		if err != nil {
			return 0, 0, nil, err
		}
		var page pdfPage
		if err := json.Unmarshal(data, &page); err != nil {
			return 0, 0, nil, err
		}
		if page.Page != n {
			return 0, 0, nil, errors.New("Persisted page identity mismatch")
		}
		if _, err := p.s.savePDFPage(d.EvidenceID, page); err != nil {
			return 0, 0, nil, err
		}
		visited[n] = true
	}
	if d.Phase == "extract" {
		entries, err := os.ReadDir(filepath.Join(p.s.root, d.EvidenceID, "digital"))
		if err != nil && !os.IsNotExist(err) {
			return 0, 0, nil, err
		}
		for _, entry := range entries {
			n, err := strconv.Atoi(strings.TrimSuffix(entry.Name(), ".json"))
			if err == nil {
				visited[n] = true
			}
		}
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
		return first, last, skip, nil
	}
	return 0, 0, nil, nil
}

func (p *pdfService) submit(ctx context.Context, d *pdfDocument, first, last int, skip []int) error {
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
	actual, err := fileDigest(p.original(d.EvidenceID))
	if err != nil || actual != d.SHA256 {
		return errors.New("Original PDF integrity failure")
	}
	control := filepath.Join(p.s.root, "control", id)
	if err := os.MkdirAll(control, 0700); err != nil {
		return err
	}
	if skip == nil {
		skip = []int{}
	}
	data, err := json.Marshal(skip)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(control, "skip.json"), data); err != nil {
		return err
	}
	a := &attemptRecord{EvidenceID: d.EvidenceID, InputSHA256: d.SHA256, Attempt: id, RequestID: id, Mode: "pdf-" + d.Engine, Location: "local-only", Status: "submitting", Started: time.Now().UTC(), WorkerVersion: "pdf-v1", WorkerSHA256: workerHash, WorkerURL: worker, Configuration: fmt.Sprintf("%s:%d-%d", d.Phase, first, last), Authorization: "allow", RequestedNodeID: node, Orchestrator: p.runtime.Client.Base, StagingDir: control}
	spec, err := p.runtime.spec(ctx, p.original(d.EvidenceID), control, id, node, d.Phase, first, last)
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
	d.Attempt = id
	d.First = first
	d.Last = last
	d.Skip = skip
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

func (p *pdfService) ingest(ctx context.Context, d *pdfDocument, a *attemptRecord) error {
	out, err := p.runtime.outputs(ctx, d.Engine, a)
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
	if info.Pages < 1 || info.Pages > 30000 || info.First != d.First || info.Last != d.Last || (d.Pages != 0 && info.Pages != d.Pages) {
		return errors.New("Worker returned invalid PDF/range metadata")
	}
	d.Pages = info.Pages
	db, err := p.s.pdfIndex(d.EvidenceID)
	if err != nil {
		return err
	}
	indexed := map[int]bool{}
	rows, err := db.Query("SELECT page FROM pages WHERE page BETWEEN ? AND ?", d.First, d.Last)
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
	ready := true
	for n := d.First; n <= d.Last; n++ {
		skipped := false
		for _, skip := range d.Skip {
			if n == skip {
				skipped = true
				break
			}
		}
		if skipped {
			continue
		}
		if indexed[n] {
			continue
		}
		if d.Phase == "extract" {
			if _, err := os.Stat(filepath.Join(p.s.root, d.EvidenceID, "digital", fmt.Sprintf("%06d.json", n))); err == nil {
				continue
			}
		}
		data, err := readPDFArtifact(filepath.Join(out, fmt.Sprintf("page-%06d.json", n)))
		if os.IsNotExist(err) {
			ready = false
			continue
		}
		if err != nil {
			return err
		}
		if len(data) > 3<<20 {
			return errors.New("Page artifact exceeds size limit")
		}
		var page pdfPage
		if err := json.Unmarshal(data, &page); err != nil {
			ready = false
			continue
		}
		if page.Page != n {
			return errors.New("Worker page reference mismatch")
		}
		if d.Phase == "extract" && page.Status == "needs_ocr" {
			if page.Source != "digital" || page.Tool != "hayro" || page.Version == "" {
				return errors.New("Invalid digital page observation")
			}
		} else {
			page.Engine = d.Engine
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
				if err := atomicWrite(path, data); err != nil {
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
			return p.runtime.acknowledge(ctx, d.Engine, a)
		}
	}
	return nil
}
