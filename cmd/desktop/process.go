package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/ufdr"
	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

const (
	evidenceLimit = 1 << 20
	inlineLimit   = 64 << 10
)

type workbench struct {
	root      string
	mu        sync.Mutex
	placement *wasijob.Placement
	pdf       *pdfService
}

type attemptRecord struct {
	EvidenceID      string                `json:"evidence_id"`
	InputSHA256     string                `json:"input_sha256"`
	Attempt         string                `json:"attempt"`
	RequestID       string                `json:"request_id"`
	RetryOf         string                `json:"retry_of,omitempty"`
	Mode            string                `json:"mode"`
	Location        string                `json:"location"`
	Status          string                `json:"status"`
	Started         time.Time             `json:"started"`
	Finished        *time.Time            `json:"finished,omitempty"`
	DurationMS      int64                 `json:"duration_ms,omitempty"`
	WorkerVersion   string                `json:"worker_version"`
	WorkerSHA256    string                `json:"worker_sha256,omitempty"`
	WorkerURL       string                `json:"worker_url,omitempty"`
	Configuration   string                `json:"configuration"`
	DelaySeconds    int                   `json:"delay_seconds,omitempty"`
	Authorization   string                `json:"authorization,omitempty"`
	RequestedNodeID string                `json:"requested_node_id,omitempty"`
	NodeID          string                `json:"node_id,omitempty"`
	JobID           string                `json:"job_id,omitempty"`
	JobState        wasijob.State[string] `json:"job_state,omitzero"`
	JobCreatedNS    int64                 `json:"job_created_unix_ns,omitempty"`
	ExecutionID     string                `json:"execution_id,omitempty"`
	Executions      []executionRecord     `json:"executions,omitempty"`
	Orchestrator    string                `json:"orchestrator,omitempty"`
	StagingDir      string                `json:"staging_dir,omitempty"`
	CancelRequested *time.Time            `json:"cancel_requested,omitempty"`
	Diagnostic      string                `json:"diagnostic,omitempty"`
	Error           string                `json:"error,omitempty"`
	Result          *ufdr.Result          `json:"result,omitempty"`
	ResultSHA256    string                `json:"result_sha256,omitempty"`
	OriginalXML     string                `json:"original_xml,omitempty"`
}

type executionRecord struct {
	ID, NodeID, PreviousExecution, NextExecution string
	CreateTime, ModifyTime                       int64
	ComputeState, DesiredState                   wasijob.State[int]
}

func (a *attemptRecord) finish(status string, err error) {
	now := time.Now().UTC()
	a.Status, a.Finished, a.DurationMS = status, &now, now.Sub(a.Started).Milliseconds()
	a.Diagnostic = ""
	if err != nil {
		a.Error = err.Error()
	}
}

func (s *workbench) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if strings.HasPrefix(r.URL.Path, "/api/pdf/") {
		s.servePDF(w, r)
		return
	}
	if r.URL.Path != "/api/process" && r.URL.Path != "/api/attempt" {
		f, _ := fs.Sub(assets, "assets")
		http.FileServer(http.FS(f)).ServeHTTP(w, r)
		return
	}
	if !s.mu.TryLock() {
		http.Error(w, "Processing already active", http.StatusConflict)
		return
	}
	defer s.mu.Unlock()

	switch {
	case r.URL.Path == "/api/process" && r.Method == http.MethodPost:
		s.process(w, r)
	case r.URL.Path == "/api/attempt" && (r.Method == http.MethodGet || r.Method == http.MethodDelete):
		s.inspect(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *workbench) process(w http.ResponseWriter, r *http.Request) {
	input, err := io.ReadAll(io.LimitReader(r.Body, evidenceLimit+1))
	if err != nil || len(input) > evidenceLimit {
		http.Error(w, "Synthetic XML must be at most 1 MiB", http.StatusBadRequest)
		return
	}
	q := r.URL.Query()
	mode, node, id := q.Get("mode"), q.Get("node"), digest(input)
	if mode != "native" && mode != "bacalhau" {
		http.Error(w, "Choose native or bacalhau", http.StatusBadRequest)
		return
	}
	attempt := strings.ToLower(rand.Text())
	a := attemptRecord{
		EvidenceID: id, InputSHA256: id, Attempt: attempt, RequestID: attempt,
		Mode: mode, Location: "local-only", Status: "submitting", Started: time.Now().UTC(),
		WorkerVersion: "ufdr-v0", Configuration: "synthetic-contract-v0",
	}
	p := s.placement
	if mode == "bacalhau" {
		a.RequestedNodeID = node
		if !p.Authorize(id, node) {
			a.Authorization = "deny"
			a.finish("denied", errors.New("evidence placement denied"))
			writeAttempt(w, http.StatusForbidden, &a)
			return
		}
		a.Authorization, a.Location = "allow", "bacalhau"
		a.Orchestrator, a.WorkerURL, a.WorkerSHA256 = p.Client.Base, p.Modules[node], p.WorkerSHA256
		if p.DelaySeconds < 0 || p.DelaySeconds > 180 {
			http.Error(w, "Invalid synthetic worker delay", http.StatusInternalServerError)
			return
		}
		a.DelaySeconds = p.DelaySeconds
	}
	v, err := ufdr.Normalize(input)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if a.RetryOf = q.Get("retry_of"); a.RetryOf != "" {
		previous, err := s.load(id, a.RetryOf)
		if err != nil || previous.Mode != mode || previous.Finished == nil ||
			(previous.Status != "failed" && previous.Status != "stopped") {
			http.Error(w, "Retry requires a failed or stopped attempt for this evidence and mode", http.StatusConflict)
			return
		}
		a.RequestID = previous.RequestID
	}
	if err = s.keepEvidence(id, input); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if mode == "native" {
		result, err := json.Marshal(v)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		a.Result, a.ResultSHA256 = &v, digest(result)
		a.finish("completed", nil)
		s.saveAndReply(w, &a, input)
		return
	}

	active, err := s.activeAttempt(&a)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if active != nil {
		s.reply(w, active, input)
		return
	}

	source := wasijob.Input{Kind: "inline", Target: "/input.xml"}
	args := []string{source.Target, "-input-sha256", id}
	if len(input) > inlineLimit {
		if p.StagingURLs[node] == "" || p.StagingDir == "" {
			http.Error(w, "Authorized input requires controlled staging", http.StatusRequestEntityTooLarge)
			return
		}
		a.StagingDir = p.StagingDir
		source.Kind, source.Target = "urldownload", "/inputs"
		filename := a.Attempt + ".xml"
		source.URL = wasijob.StageURL(p.StagingURLs[node], "evidence/"+filename)
		args[0] = "/inputs/" + filename
	}
	if a.StagingDir == "" {
		source.URL = wasijob.InlineXML(input)
	}
	if a.StagingDir != "" || len(mustResultJSON(v)) > 1900 {
		a.Configuration = "synthetic-contract-v0+gzip"
		args = append(args, "-gzip")
	}
	if a.DelaySeconds > 0 {
		args = append(args, "-delay-seconds", strconv.Itoa(a.DelaySeconds))
	}
	if err = s.save(&a); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if a.StagingDir != "" {
		if err = atomicWrite(a.stagedPath(), input); err != nil {
			a.finish("failed", fmt.Errorf("stage input: %w", err))
			s.saveAndReply(w, &a, input)
			return
		}
	}

	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	spec := wasijob.Spec(a.jobName(), node, a.WorkerURL, source, args, 35+a.DelaySeconds)
	a.JobID, err = p.Client.Submit(ctx, spec)
	if err != nil {
		a.Diagnostic = "Submission outcome unknown: " + err.Error()
	} else {
		a.Status = "accepted"
	}
	s.saveAndReply(w, &a, input)
}

func (a *attemptRecord) jobName() string { return "viii-" + a.Attempt }

func (a *attemptRecord) stagedPath() string {
	return filepath.Join(a.StagingDir, a.Attempt+".xml")
}

func (s *workbench) inspect(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	a, err := s.load(q.Get("evidence"), q.Get("attempt"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	input, err := os.ReadFile(filepath.Join(s.root, a.EvidenceID, "original.xml"))
	if err != nil || digest(input) != a.InputSHA256 || a.EvidenceID != a.InputSHA256 {
		http.Error(w, "Evidence integrity failure", http.StatusInternalServerError)
		return
	}
	if a.Finished != nil {
		s.removeStagedInput(a)
		s.reply(w, a, input)
		return
	}
	if r.Method == http.MethodDelete && a.CancelRequested == nil {
		now := time.Now().UTC()
		a.CancelRequested = &now
		if err = s.save(a); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}
	if s.placement == nil || s.placement.Client.Base != a.Orchestrator {
		a.Diagnostic = "Original orchestrator configuration unavailable"
		s.saveAndReply(w, a, input)
		return
	}
	client := s.placement.Client
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()

	key := a.JobID
	if key == "" {
		key = a.jobName()
	}
	d, err := client.Describe(ctx, key)
	if err != nil {
		a.Diagnostic = "Job reconciliation pending: " + err.Error()
		s.saveAndReply(w, a, input)
		return
	}
	if d.Job.ID == "" || d.Job.Name != a.jobName() || (a.JobID != "" && d.Job.ID != a.JobID) {
		a.Diagnostic = "Orchestrator returned a different job identity"
		s.saveAndReply(w, a, input)
		return
	}
	a.JobID, a.JobState, a.JobCreatedNS = d.Job.ID, d.Job.State, d.Job.CreateTime
	a.Status, a.Diagnostic = "accepted", ""
	if a.JobState.StateType == "Running" {
		a.Status = "running"
	}
	a.Executions, a.ExecutionID, a.NodeID = nil, "", ""
	for _, x := range d.Executions.Items {
		a.Executions = append(a.Executions, executionRecord{
			ID: x.ID, NodeID: x.NodeID, PreviousExecution: x.PreviousExecution, NextExecution: x.NextExecution,
			CreateTime: x.CreateTime, ModifyTime: x.ModifyTime, ComputeState: x.ComputeState, DesiredState: x.DesiredState,
		})
	}
	if len(a.Executions) == 1 {
		a.ExecutionID, a.NodeID = a.Executions[0].ID, a.Executions[0].NodeID
	}
	switch a.JobState.StateType {
	case "Completed":
		if err = a.acceptResult(d, input); err != nil {
			a.finish("failed", err)
		} else {
			a.finish("completed", nil)
		}
	case "Failed":
		a.finish("failed", fmt.Errorf("Bacalhau job failed: %s", a.JobState.Message))
	case "Stopped", "Cancelled", "Canceled":
		a.finish("stopped", fmt.Errorf("Bacalhau job %s: %s", a.JobState.StateType, a.JobState.Message))
	default:
		for _, x := range a.Executions {
			if x.NodeID != "" && x.NodeID != a.RequestedNodeID {
				a.Diagnostic = fmt.Sprintf("Unexpected execution node %s (authorized %s)", x.NodeID, a.RequestedNodeID)
				break
			}
		}
		if a.CancelRequested != nil {
			if err = client.Stop(ctx, a.JobID); err != nil {
				a.Diagnostic = "Cancellation request failed: " + err.Error()
			} else {
				a.Diagnostic = "Cancellation requested; waiting for terminal state"
			}
		}
	}
	s.saveAndReply(w, a, input)
}

func mustResultJSON(v ufdr.Result) []byte {
	b, _ := json.Marshal(v) // The result contains only strings, pointers and slices.
	return b
}

func (a *attemptRecord) acceptResult(d wasijob.Description, input []byte) error {
	output, err := d.Result(a.RequestedNodeID)
	if err != nil {
		return err
	}
	if a.Configuration == "synthetic-contract-v0+gzip" {
		output, err = wasijob.DecodeResult(output)
		if err != nil {
			return err
		}
	}
	expected, err := ufdr.Normalize(input)
	if err != nil {
		return err
	}
	if !bytes.Equal(bytes.TrimSpace(output), mustResultJSON(expected)) {
		return errors.New("worker result differs from source normalization")
	}
	a.Result, a.ResultSHA256 = &expected, digest(bytes.TrimSpace(output))
	return nil
}

func (s *workbench) keepEvidence(id string, input []byte) error {
	dir := filepath.Join(s.root, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "original.xml")
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return atomicWrite(path, input)
	}
	if err == nil && !bytes.Equal(b, input) {
		return errors.New("evidence integrity failure")
	}
	return err
}

func (s *workbench) activeAttempt(want *attemptRecord) (*attemptRecord, error) {
	entries, err := os.ReadDir(filepath.Join(s.root, want.EvidenceID))
	if err != nil {
		return nil, err
	}
	for _, item := range entries {
		id := strings.TrimSuffix(item.Name(), ".json")
		if id == item.Name() || !validAttempt(id) {
			continue
		}
		a, err := s.load(want.EvidenceID, id)
		if err != nil {
			return nil, err
		}
		sameRetry := want.RetryOf != "" && a.RetryOf == want.RetryOf && a.RequestedNodeID == want.RequestedNodeID
		sameActiveJob := a.Finished == nil &&
			((want.RetryOf == "" && a.RequestedNodeID == want.RequestedNodeID) ||
				(want.RetryOf != "" && a.RequestID == want.RequestID))
		if a.Mode == want.Mode && (sameRetry || sameActiveJob) {
			return a, nil
		}
	}
	return nil, nil
}

func validAttempt(id string) bool {
	return len(id) == 26 && strings.Trim(id, "abcdefghijklmnopqrstuvwxyz234567") == ""
}

func (s *workbench) load(evidence, attempt string) (*attemptRecord, error) {
	if _, err := hex.DecodeString(evidence); err != nil || len(evidence) != 64 || !validAttempt(attempt) {
		return nil, errors.New("invalid evidence or attempt identifier")
	}
	base := filepath.Join(s.root, evidence, attempt)
	snapshot, _, err := latestSnapshot(base)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(snapshot)
	if err != nil {
		return nil, err
	}
	var a attemptRecord
	if err = json.Unmarshal(b, &a); err == nil && (a.EvidenceID != evidence || a.Attempt != attempt) {
		err = errors.New("attempt identity mismatch")
	}
	return &a, err
}

func (s *workbench) save(a *attemptRecord) error {
	b, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	base := filepath.Join(s.root, a.EvidenceID, a.Attempt)
	current, revision, err := latestSnapshot(base)
	if err != nil {
		return err
	}
	previous, err := os.ReadFile(current)
	if os.IsNotExist(err) && revision == 0 {
		return atomicWrite(current, b)
	}
	if err != nil || bytes.Equal(previous, b) {
		return err
	}
	if err = os.MkdirAll(base, 0700); err != nil {
		return err
	}
	return atomicWrite(filepath.Join(base, fmt.Sprintf("%08d.json", revision+1)), b)
}

// Keep committed observations immutable. The first snapshot is the intent
// written before submission. A reader can reopen it while updates are saved.
func latestSnapshot(base string) (string, int, error) {
	entries, err := os.ReadDir(base)
	if err != nil && !os.IsNotExist(err) {
		return "", 0, err
	}
	for i := len(entries) - 1; i >= 0; i-- {
		name := entries[i].Name()
		if len(name) == len("00000001.json") && strings.HasSuffix(name, ".json") {
			revision, err := strconv.Atoi(strings.TrimSuffix(name, ".json"))
			if err == nil && revision > 0 {
				return filepath.Join(base, name), revision, nil
			}
		}
	}
	return base + ".json", 0, nil
}

func atomicWrite(path string, data []byte) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".viii-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, writeErr := f.Write(data)
	err = errors.Join(writeErr, f.Sync(), f.Close())
	if err != nil {
		return err
	}
	err = os.Rename(f.Name(), path)
	// Windows readers can briefly deny replacement while holding the old file.
	for attempt := 0; err != nil && runtime.GOOS == "windows" && attempt < 5; attempt++ {
		time.Sleep(10 * time.Millisecond << attempt)
		err = os.Rename(f.Name(), path)
	}
	return err
}

func (s *workbench) saveAndReply(w http.ResponseWriter, a *attemptRecord, input []byte) {
	if err := s.save(a); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.removeStagedInput(a)
	s.reply(w, a, input)
}

func (s *workbench) removeStagedInput(a *attemptRecord) {
	if a.Finished != nil && a.StagingDir != "" {
		if err := os.Remove(a.stagedPath()); err != nil && !os.IsNotExist(err) {
			a.Diagnostic = "Staging cleanup failed: " + err.Error()
		}
	}
}

func (s *workbench) reply(w http.ResponseWriter, a *attemptRecord, input []byte) {
	status := http.StatusOK
	if a.Finished == nil {
		status = http.StatusAccepted
	}
	response := *a
	if a.Result != nil {
		response.OriginalXML = string(input)
	}
	writeAttempt(w, status, &response)
}

func writeAttempt(w http.ResponseWriter, status int, a *attemptRecord) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(a)
}
