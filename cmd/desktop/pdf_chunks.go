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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Large PDFs are prepared once per document by a Bacalhau WASI job (flpdf,
// ADR-0003) into fixed page-range chunks. Chunks are operational derivatives
// bound to the original's sha256: page jobs mount a chunk instead of the
// original, and every result is mapped back to the original's physical pages.
const (
	chunkingPreparing   = "preparing"
	chunkingReady       = "ready"
	chunkingUnavailable = "unavailable"
	defaultChunkSize    = 40
	maxPrepareFailures  = 2
	flpdfVersion        = "0.10.0"
	// defaultPDFJobs: 4 simultaneous chunk jobs gave 3.4x the OCR throughput of 1
	// with unchanged UI latency (docs/poc/pdf-chunking.md, 1/2/4 measurement).
	defaultPDFJobs = 4
)

type chunkPage struct {
	Local    int `json:"local"`
	Physical int `json:"physical"`
}

// pdfChunk is the worker's sidecar plus the host's verification record.
type pdfChunk struct {
	Schema       string      `json:"schema"`
	Tool         string      `json:"tool"`
	ToolVersion  string      `json:"tool_version"`
	ExtractRoute string      `json:"extract_route"`
	SourcePages  int         `json:"source_page_count"`
	SourceSHA256 string      `json:"source_sha256_declared"`
	ChunkSize    int         `json:"chunk_size"`
	File         string      `json:"file"`
	First        int         `json:"physical_first"`
	Last         int         `json:"physical_last"`
	Count        int         `json:"chunk_page_count"`
	Pages        []chunkPage `json:"pages"`
	SHA256       string      `json:"chunk_sha256"`
	Bytes        int64       `json:"chunk_bytes"`

	EvidenceID     string     `json:"evidence_id,omitempty"`
	PrepareAttempt string     `json:"prepare_attempt,omitempty"`
	JobID          string     `json:"job_id,omitempty"`
	ExecutionID    string     `json:"execution_id,omitempty"`
	ChunkerSHA256  string     `json:"chunker_sha256,omitempty"`
	Verified       *time.Time `json:"verified,omitempty"`
}

func (p *pdfService) chunkDir(id string) string { return filepath.Join(p.s.root, "chunks", id) }

func (p *pdfService) chunkSize(d *pdfDocument) int {
	if d.ChunkSize > 0 {
		return d.ChunkSize
	}
	return defaultChunkSize
}

// validChunk checks a sidecar against the document before any byte is trusted.
func validChunk(d *pdfDocument, c *pdfChunk, size int) error {
	n := c.Last - c.First + 1
	switch {
	case c.Schema != "viii.pdf-chunk-sidecar.v1" || c.Tool != "flpdf" || c.ToolVersion != flpdfVersion:
		return errors.New("unsupported chunk tool or schema")
	case c.SourceSHA256 != d.SHA256:
		return errors.New("chunk is not bound to this original")
	case c.SourcePages < 1 || c.SourcePages > 30000 || (d.Pages != 0 && c.SourcePages != d.Pages):
		return errors.New("chunk source page count mismatch")
	case c.ChunkSize != size || c.First < 1 || (c.First-1)%size != 0 || c.Last != min(c.First+size-1, c.SourcePages):
		return errors.New("chunk boundaries do not follow the chunk plan")
	case c.File != fmt.Sprintf("chunk-%06d-%06d.pdf", c.First, c.Last) || c.Count != n || len(c.Pages) != n:
		return errors.New("chunk identity mismatch")
	case len(c.SHA256) != 64 || c.Bytes < 1:
		return errors.New("chunk digest missing")
	}
	if _, err := hex.DecodeString(c.SHA256); err != nil {
		return errors.New("chunk digest missing")
	}
	for i, m := range c.Pages {
		if m.Local != i+1 || m.Physical != c.First+i {
			return errors.New("chunk local to physical map mismatch")
		}
	}
	return nil
}

// chunks returns the verified chunks of a document, ordered by physical page.
func (p *pdfService) chunks(d *pdfDocument) ([]pdfChunk, error) {
	entries, err := os.ReadDir(p.chunkDir(d.EvidenceID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []pdfChunk
	for _, e := range entries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "chunk-") || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := readPDFArtifact(filepath.Join(p.chunkDir(d.EvidenceID), e.Name()))
		if err != nil {
			return nil, err
		}
		var c pdfChunk
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, err
		}
		if err := validChunk(d, &c, p.chunkSize(d)); err != nil || c.EvidenceID != d.EvidenceID || strings.TrimSuffix(c.File, ".pdf")+".json" != e.Name() {
			return nil, fmt.Errorf("invalid stored chunk record %s", e.Name())
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].First < out[j].First })
	return out, nil
}

// copyVerified copies src to dst (a new file) and returns its sha256 and size.
func copyVerified(src, dst string) (string, int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return "", 0, err
	}
	defer in.Close()
	tmp, err := os.CreateTemp(filepath.Dir(dst), ".chunk-*")
	if err != nil {
		return "", 0, err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), in)
	if err = errors.Join(err, tmp.Sync(), tmp.Close()); err != nil {
		return "", 0, err
	}
	sum := hex.EncodeToString(h.Sum(nil))
	return sum, n, renameRetry(tmp.Name(), dst)
}

func renameRetry(from, to string) error {
	var err error
	for i := 0; i < 20; i++ {
		if err = os.Rename(from, to); err == nil {
			return nil
		}
		time.Sleep(25 * time.Millisecond)
	}
	return err
}

// storeChunk copies one chunk out of the preparation job's result directory
// and verifies it before Bacalhau can remove it. Stored chunks are immutable:
// a repeated preparation must reproduce identical bytes.
func (p *pdfService) storeChunk(d *pdfDocument, a *attemptRecord, out, sidecar string) (bool, error) {
	data, err := readPDFArtifact(filepath.Join(out, sidecar))
	if err != nil {
		return false, err
	}
	var c pdfChunk
	if json.Unmarshal(data, &c) != nil {
		return false, nil // the sidecar may still be being written
	}
	if err := validChunk(d, &c, p.chunkSize(d)); err != nil {
		return false, err
	}
	dir := p.chunkDir(d.EvidenceID)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return false, err
	}
	record := filepath.Join(dir, strings.TrimSuffix(c.File, ".pdf")+".json")
	if existing, err := readPDFArtifact(record); err == nil {
		var stored pdfChunk
		if json.Unmarshal(existing, &stored) != nil || stored.SHA256 != c.SHA256 || stored.First != c.First || stored.Last != c.Last {
			return false, errors.New("Repeated preparation produced a different chunk")
		}
		return true, nil
	}
	pdf := filepath.Join(dir, c.File)
	if _, err := os.Stat(pdf); err == nil {
		if got, err := fileDigest(pdf); err != nil || got != c.SHA256 {
			return false, errors.New("Stored chunk integrity failure")
		}
	} else {
		sum, n, err := copyVerified(filepath.Join(out, c.File), pdf)
		if err != nil {
			return false, err
		}
		if sum != c.SHA256 || n != c.Bytes {
			os.Remove(pdf)
			return false, errors.New("Copied chunk does not match its sidecar")
		}
	}
	now := time.Now().UTC()
	c.EvidenceID, c.PrepareAttempt, c.JobID, c.ExecutionID, c.ChunkerSHA256, c.Verified = d.EvidenceID, a.Attempt, a.JobID, a.ExecutionID, a.WorkerSHA256, &now
	data, err = json.Marshal(c)
	if err != nil {
		return false, err
	}
	if err := atomicWrite(record, data); err != nil {
		return false, err
	}
	if d.Pages == 0 {
		d.Pages = c.SourcePages
	}
	return true, nil
}

// nextPreparePage is the first physical page not covered by stored chunks.
func nextPreparePage(chunks []pdfChunk) int {
	next := 1
	for _, c := range chunks {
		if c.First != next {
			break
		}
		next = c.Last + 1
	}
	return next
}

func (p *pdfService) preparing(d *pdfDocument) bool {
	return d.Chunking != chunkingReady && d.Chunking != chunkingUnavailable
}

// advancePrepare submits, reconciles and ingests the preparation job. It never
// blocks page jobs on chunks that are already verified.
func (p *pdfService) advancePrepare(ctx context.Context, d *pdfDocument) error {
	if d.Prepare == "" && !p.preparing(d) {
		return nil
	}
	// A ready chunk set still reconciles its job until it is terminal.
	if p.preparing(d) {
		if p.runtime.Chunker == "" {
			d.Chunking = chunkingUnavailable
			d.ChunkDiagnostic = "Chunker not bundled; processing the original in place"
			return nil
		}
		d.Chunking = chunkingPreparing
	}
	if d.Prepare == "" {
		if d.Paused || d.Cancel {
			return nil
		}
		return p.submitPrepare(ctx, d)
	}
	a, err := p.s.load(d.EvidenceID, d.Prepare)
	if err != nil {
		return err
	}
	description, err := p.describe(ctx, d, a)
	if err != nil || description == nil {
		return err
	}
	if len(a.Executions) == 1 && d.Chunking == chunkingPreparing {
		if err := p.ingestPrepare(ctx, d, a); err != nil {
			return err
		}
	}
	if d.Cancel && a.CancelRequested == nil && d.Chunking != chunkingReady {
		now := time.Now().UTC()
		a.CancelRequested = &now
	}
	if !terminal(description, a) {
		if a.CancelRequested != nil {
			if err := p.runtime.Client.Stop(ctx, a.JobID); err != nil {
				return err
			}
		}
		return p.s.save(a)
	}
	switch {
	case d.Chunking == chunkingReady:
		a.finish("completed", nil)
	case a.CancelRequested != nil:
		a.finish("stopped", nil)
	default:
		reason := "Bacalhau preparation job failed"
		for _, x := range description.Executions.Items {
			if x.RunOutput != nil && (x.RunOutput.Stderr != "" || x.RunOutput.ErrorMsg != "") {
				reason = strings.TrimSpace(x.RunOutput.Stderr + " " + x.RunOutput.ErrorMsg)
			}
		}
		a.finish("failed", errors.New(reason))
		d.PrepareFailures++
		if d.PrepareFailures >= maxPrepareFailures {
			d.Chunking = chunkingUnavailable
			d.ChunkDiagnostic = "Preparação em chunks indisponível (" + reason + "); processando o original diretamente"
		}
	}
	d.Prepare = ""
	return p.s.save(a)
}

func (p *pdfService) submitPrepare(ctx context.Context, d *pdfDocument) error {
	node := p.runtime.node("wasi")
	if !d.Authorized[node] {
		return errors.New("Evidence placement denied")
	}
	actual, err := fileDigest(p.original(d.EvidenceID))
	if err != nil || actual != d.SHA256 {
		return errors.New("Original PDF integrity failure")
	}
	chunks, err := p.chunks(d)
	if err != nil {
		return err
	}
	first, last := nextPreparePage(chunks), 0
	if len(chunks) > 0 {
		last = d.Pages
	}
	if first > 1 && first > last {
		return p.finishPreparation(ctx, d, nil, chunks, nil)
	}
	d.ChunkSize = p.chunkSize(d)
	id := strings.ToLower(rand.Text())
	control := filepath.Join(p.s.root, "control", id)
	if err := os.MkdirAll(control, 0700); err != nil {
		return err
	}
	a := &attemptRecord{EvidenceID: d.EvidenceID, InputSHA256: d.SHA256, Attempt: id, RequestID: id, Mode: "pdf-chunk-wasi", Location: "local-only", Status: "submitting", Started: time.Now().UTC(), WorkerVersion: "pdf-chunker-flpdf-" + flpdfVersion, WorkerSHA256: p.runtime.ChunkerSHA, WorkerURL: p.runtime.Chunker, Configuration: fmt.Sprintf("prepare:chunk-size=%d:from=%d", d.ChunkSize, first), Authorization: "allow", RequestedNodeID: node, Orchestrator: p.runtime.Client.Base, StagingDir: control}
	spec, err := p.runtime.prepareSpec(p.original(d.EvidenceID), control, id, node, d.SHA256, d.ChunkSize, first, last)
	if err != nil {
		return err
	}
	data, err := json.Marshal(spec)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(control, "submission.json"), data); err != nil {
		return err
	}
	if err := p.s.save(a); err != nil {
		return err
	}
	d.Prepare = id
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

// ingestPrepare stores every newly verified chunk and, after `ready`, checks
// the chunk-set manifest against the stored chunks before acknowledging.
func (p *pdfService) ingestPrepare(ctx context.Context, d *pdfDocument, a *attemptRecord) error {
	out, err := p.runtime.outputs(ctx, "wasi", a)
	if err != nil {
		return err
	}
	entries, err := os.ReadDir(out)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	pending := 0
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "chunk-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		ok, err := p.storeChunk(d, a, out, name)
		if err != nil {
			return err
		}
		if !ok {
			pending++
		}
	}
	if pending > 0 {
		return nil
	}
	if data, err := os.ReadFile(filepath.Join(out, "ready")); err != nil || string(data) != "ready" {
		return nil
	}
	f, err := os.Open(filepath.Join(out, "manifest.json"))
	if err != nil {
		return err
	}
	data, err := io.ReadAll(io.LimitReader(f, 32<<20))
	f.Close()
	if err != nil {
		return err
	}
	var manifest struct {
		Schema       string     `json:"schema"`
		Tool         string     `json:"tool"`
		ToolVersion  string     `json:"tool_version"`
		SourcePages  int        `json:"source_page_count"`
		SourceSHA256 string     `json:"source_sha256_declared"`
		First        int        `json:"physical_first"`
		Last         int        `json:"physical_last"`
		ChunkSize    int        `json:"chunk_size"`
		Chunks       []pdfChunk `json:"chunks"`
		Diagnostics  []string   `json:"diagnostics"`
		Route        string     `json:"extract_route"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return err
	}
	if manifest.Schema != "viii.pdf-chunk-set.v1" || manifest.Tool != "flpdf" || manifest.ToolVersion != flpdfVersion || manifest.SourceSHA256 != d.SHA256 || manifest.SourcePages != d.Pages || manifest.ChunkSize != d.ChunkSize || manifest.Last != d.Pages {
		return errors.New("Chunk-set manifest does not match the document")
	}
	chunks, err := p.chunks(d)
	if err != nil {
		return err
	}
	stored := map[string]pdfChunk{}
	for _, c := range chunks {
		stored[c.File] = c
	}
	next := manifest.First
	for _, c := range manifest.Chunks {
		s, ok := stored[c.File]
		if !ok || s.SHA256 != c.SHA256 || s.First != next || s.Last != c.Last {
			return errors.New("Chunk-set manifest does not match the stored chunks")
		}
		next = c.Last + 1
	}
	if next != d.Pages+1 {
		return errors.New("Chunk-set manifest does not cover the document")
	}
	return p.finishPreparation(ctx, d, a, chunks, manifest.Diagnostics)
}

// finishPreparation records the complete chunk set and acknowledges the job.
func (p *pdfService) finishPreparation(ctx context.Context, d *pdfDocument, a *attemptRecord, chunks []pdfChunk, diagnostics []string) error {
	next := 1
	for _, c := range chunks {
		if c.First != next {
			return errors.New("Stored chunks do not tile the document")
		}
		next = c.Last + 1
	}
	if next != d.Pages+1 {
		return errors.New("Stored chunks do not tile the document")
	}
	set := map[string]any{"schema": "viii.pdf-chunk-set-record.v1", "evidence_id": d.EvidenceID, "pages": d.Pages, "chunk_size": d.ChunkSize, "chunks": len(chunks), "tool": "flpdf", "tool_version": flpdfVersion, "chunker_sha256": p.runtime.ChunkerSHA, "diagnostics": diagnostics, "completed": time.Now().UTC()}
	if a != nil {
		set["prepare_attempt"], set["job_id"], set["execution_id"] = a.Attempt, a.JobID, a.ExecutionID
	}
	data, err := json.MarshalIndent(set, "", "  ")
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(p.chunkDir(d.EvidenceID), "set.json"), data); err != nil {
		return err
	}
	d.Chunking = chunkingReady
	if len(diagnostics) > 0 {
		d.ChunkDiagnostic = "Reparos estruturais registrados na preparação: " + strings.Join(diagnostics, "; ")
	}
	if err := p.save(d); err != nil {
		return err
	}
	if a == nil {
		return nil
	}
	return p.runtime.acknowledge(ctx, "wasi", a)
}

// nextRange returns the next unprocessed physical range of the current phase
// inside one verified chunk, excluding pages owned by active jobs.
func (p *pdfService) nextRange(d *pdfDocument, visited map[int]bool, chunks []pdfChunk) (*pdfRange, *pdfChunk) {
	for i := range chunks {
		c := &chunks[i]
		for first := c.First; first <= c.Last; first++ {
			if visited[first] {
				continue
			}
			r := &pdfRange{First: first, Last: c.Last, Skip: []int{}}
			for n := first; n <= c.Last; n++ {
				if visited[n] {
					r.Skip = append(r.Skip, n)
				}
			}
			return r, c
		}
	}
	return nil, nil
}
