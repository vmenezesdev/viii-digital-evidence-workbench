package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/ufdr"
	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

func TestCompletedWorkerValidation(t *testing.T) {
	input := readSpikeFile(t, "../../docs/research/fixtures/ufdr/messages-minimal.xml")
	v, err := ufdr.Normalize(input)
	if err != nil {
		t.Fatal(err)
	}
	good := string(mustResultJSON(v))
	for _, tc := range []struct {
		name, node, output       string
		exit                     int
		truncated, missing, gzip bool
	}{
		{name: "valid", node: "allowed", output: good},
		{name: "unauthorized", node: "intruder", output: good},
		{name: "nonzero exit", node: "allowed", output: good, exit: 1},
		{name: "truncated", node: "allowed", output: good, truncated: true},
		{name: "missing", node: "allowed", missing: true},
		{name: "empty", node: "allowed"},
		{name: "malformed", node: "allowed", output: "{"},
		{name: "mismatched", node: "allowed", output: strings.ReplaceAll(good, "library", "airport")},
		{name: "bad gzip", node: "allowed", output: "not-base64", gzip: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var name string
			client := testOrchestrator(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodPut {
					var req struct{ Job struct{ Name string } }
					if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
						t.Fatal(err)
					}
					name = req.Job.Name
					w.Write([]byte(`{"JobID":"job"}`))
					return
				}
				x := map[string]any{"ID": "execution", "NodeID": tc.node}
				if !tc.missing {
					x["RunOutput"] = map[string]any{"Stdout": tc.output, "ExitCode": tc.exit, "StdoutTruncated": tc.truncated}
				}
				json.NewEncoder(w).Encode(map[string]any{"Job": map[string]any{"ID": "job", "Name": name, "State": map[string]string{"StateType": "Completed"}}, "Executions": map[string]any{"Items": []any{x}}})
			})
			p := &wasijob.Placement{Allow: map[string]map[string]bool{digest(input): {"allowed": true}}, Modules: map[string]string{"allowed": "http://stage/worker.wasm"}, WorkerSHA256: "worker", Client: client}
			s := &workbench{root: t.TempDir(), placement: p}
			a := callAttempt(t, s, "POST", "/api/process?mode=bacalhau&node=allowed", input, 202)
			if tc.gzip {
				r, err := s.load(a["evidence_id"].(string), a["attempt"].(string))
				if err != nil {
					t.Fatal(err)
				}
				r.Configuration = "synthetic-contract-v0+gzip"
				if err = s.save(r); err != nil {
					t.Fatal(err)
				}
			}
			a = callAttempt(t, s, "GET", attemptPath(a), nil, 200)
			if tc.name == "valid" {
				if a["status"] != "completed" || a["result"] == nil {
					t.Fatalf("valid result rejected: %v", a)
				}
			} else if a["status"] != "failed" || a["result"] != nil || a["error"] == nil {
				t.Fatalf("invalid result accepted: %v", a)
			}
		})
	}
}

func TestEvidenceIntegrityAndIdentifierBoundaries(t *testing.T) {
	input := readSpikeFile(t, "../../docs/research/fixtures/ufdr/messages-minimal.xml")
	s := &workbench{root: t.TempDir()}
	a := callAttempt(t, s, "POST", "/api/process?mode=native", input, 200)
	for _, ids := range [][2]string{{"../escape", a["attempt"].(string)}, {digest(input), "../escape"}, {strings.Repeat("z", 64), a["attempt"].(string)}} {
		if _, err := s.load(ids[0], ids[1]); err == nil {
			t.Fatal("accepted invalid identifier", ids)
		}
	}
	path := filepath.Join(s.root, digest(input), "original.xml")
	corrupt := []byte("corrupted synthetic evidence")
	if err := os.WriteFile(path, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("GET", attemptPath(a), nil))
	if w.Code != 500 || !strings.Contains(w.Body.String(), "integrity") {
		t.Fatalf("corruption not detected: %d %s", w.Code, w.Body)
	}
	w = httptest.NewRecorder()
	s.ServeHTTP(w, httptest.NewRequest("POST", "/api/process?mode=native", bytes.NewReader(input)))
	if w.Code != 500 || !bytes.Equal(readSpikeFile(t, path), corrupt) {
		t.Fatal("silently replaced corrupt evidence")
	}
}

func TestSnapshotFailurePreservesHistory(t *testing.T) {
	input := readSpikeFile(t, "../../docs/research/fixtures/ufdr/messages-minimal.xml")
	s := &workbench{root: t.TempDir()}
	a := callAttempt(t, s, "POST", "/api/process?mode=native", input, 200)
	id, attempt := a["evidence_id"].(string), a["attempt"].(string)
	r, err := s.load(id, attempt)
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(s.root, id, attempt)
	// Block revision-directory creation on every supported platform.
	if err = os.WriteFile(base, []byte("obstacle"), 0600); err != nil {
		t.Fatal(err)
	}
	r.Diagnostic = "must not replace prior snapshot"
	if err = s.save(r); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if err = os.Remove(base); err != nil {
		t.Fatal(err)
	}
	// An interrupted temporary write is never a committed revision.
	if err = os.Mkdir(base, 0700); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(base, ".viii-interrupted"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	restarted := &workbench{root: s.root}
	got := callAttempt(t, restarted, "GET", attemptPath(a), nil, 200)
	if got["diagnostic"] != nil || got["result_sha256"] != a["result_sha256"] || got["evidence_id"] != id {
		t.Fatalf("lost valid history: %v", got)
	}
}

func TestExplicitRetryLineage(t *testing.T) {
	input := readSpikeFile(t, "../../docs/research/fixtures/ufdr/messages-minimal.xml")
	submissions := 0
	client := testOrchestrator(func(w http.ResponseWriter, r *http.Request) { submissions++; w.Write([]byte(`{"JobID":"retry-job"}`)) })
	p := &wasijob.Placement{Allow: map[string]map[string]bool{digest(input): {"allowed": true}}, Modules: map[string]string{"allowed": "http://stage/worker.wasm"}, WorkerSHA256: "worker", Client: client}
	s := &workbench{root: t.TempDir(), placement: p}
	a := callAttempt(t, s, "POST", "/api/process?mode=native", input, 200)
	id, attempt := a["evidence_id"].(string), a["attempt"].(string)
	r, err := s.load(id, attempt)
	if err != nil {
		t.Fatal(err)
	}
	r.Mode = "bacalhau"
	r.Status = "failed"
	if err = s.save(r); err != nil {
		t.Fatal(err)
	}
	s = &workbench{root: s.root, placement: p}
	url := "/api/process?mode=bacalhau&node=allowed&retry_of=" + attempt
	retry := callAttempt(t, s, "POST", url, input, 202)
	if retry["retry_of"] != attempt || retry["request_id"] != a["request_id"] || retry["attempt"] == attempt || retry["evidence_id"] != id {
		t.Fatalf("lost retry lineage: %v", retry)
	}
	if duplicate := callAttempt(t, s, "POST", url, input, 202); duplicate["attempt"] != retry["attempt"] || submissions != 1 {
		t.Fatal("duplicate retry submission")
	}
	old, err := s.load(id, attempt)
	if err != nil || old.Status != "failed" {
		t.Fatal("retry changed prior attempt", err)
	}
}
