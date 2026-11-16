// VIII development PoC. Synthetic inputs only; no remote execution is authorized.
package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/ufdr"
	"github.com/wailsapp/wails/v3/pkg/application"
	"io"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

//go:embed assets/*
var assets embed.FS

type workbench struct {
	root string
	mu   sync.Mutex
}

func digest(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func (s *workbench) cli(ctx context.Context, args ...string) ([]byte, error) {
	args = append(args, "--data-dir", filepath.Join(s.root, "client"), "--api-host", "127.0.0.1", "--api-port", "2234")
	c := exec.CommandContext(ctx, `C:\Bacalhau\bin\bacalhau.exe`, args...)
	b, e := c.CombinedOutput()
	if e != nil {
		return nil, fmt.Errorf("Bacalhau: %w: %s", e, b)
	}
	return b, nil
}
func (s *workbench) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/api/process" {
		f, _ := fs.Sub(assets, "assets")
		http.FileServer(http.FS(f)).ServeHTTP(w, r)
		return
	}
	if r.Method != "POST" {
		http.Error(w, "POST required", 405)
		return
	}
	if !s.mu.TryLock() {
		http.Error(w, "Processing already active", 409)
		return
	}
	defer s.mu.Unlock()
	input, e := io.ReadAll(io.LimitReader(r.Body, 1024*1024+1))
	if e != nil || len(input) > 1024*1024 {
		http.Error(w, "Synthetic XML must be at most 1 MiB", 400)
		return
	}
	if _, e = ufdr.Normalize(input); e != nil {
		http.Error(w, e.Error(), 400)
		return
	}
	id := digest(input)
	evidence := filepath.Join(s.root, id)
	if e = os.MkdirAll(evidence, 0700); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	original := filepath.Join(evidence, "original.xml")
	if b, err := os.ReadFile(original); err == nil {
		if digest(b) != id {
			http.Error(w, "Evidence integrity failure", 500)
			return
		}
	} else if !os.IsNotExist(err) {
		http.Error(w, err.Error(), 500)
		return
	} else if e = os.WriteFile(original, input, 0600); e != nil {
		http.Error(w, e.Error(), 500)
		return
	}
	started := time.Now().UTC()
	attempt := fmt.Sprint(started.UnixNano())
	record := map[string]any{"evidence_id": id, "input_sha256": id, "worker_version": "ufdr-v0", "configuration": "synthetic-contract-v0", "started": started, "location": "local-only", "mode": r.URL.Query().Get("mode"), "attempt": attempt}
	var result []byte
	if record["mode"] == "native" {
		v, err := ufdr.Normalize(input)
		e = err
		if e == nil {
			result, e = json.Marshal(v)
		}
	} else if record["mode"] == "bacalhau" {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		source := func(kind, url, target string) any {
			return map[string]any{"Source": map[string]any{"Type": kind, "Params": map[string]string{"URL": url}}, "Target": target}
		}
		spec := map[string]any{"Name": "viii-" + attempt, "Type": "batch", "Count": 1, "Tasks": []any{map[string]any{"Name": "main", "Engine": map[string]any{"Type": "wasm", "Params": map[string]any{"EntryModule": "/worker.wasm", "Entrypoint": "_start", "Parameters": []string{"/input.xml"}}}, "InputSources": []any{source("urldownload", "http://127.0.0.1:28080/worker.wasm", "/worker.wasm"), source("inline", "data:text/xml;base64,"+base64.StdEncoding.EncodeToString(input), "/input.xml")}}}}
		b, _ := json.Marshal(spec)
		path := filepath.Join(evidence, attempt+"-request.json")
		e = os.WriteFile(path, b, 0600)
		if e == nil {
			var out []byte
			out, e = s.cli(ctx, "job", "run", path, "--no-template", "--id-only", "--wait=false")
			if e == nil {
				job := strings.TrimSpace(string(out))
				lines := strings.Split(job, "\n")
				job = strings.TrimSpace(lines[len(lines)-1])
				record["job_id"] = job
				for e == nil {
					var desc []byte
					desc, e = s.cli(ctx, "job", "describe", job, "--output", "json")
					if e != nil {
						break
					}
					var d struct {
						Job        struct{ State struct{ StateType string } }
						Executions struct {
							Items []struct {
								ID        string
								NodeID    string
								RunOutput struct {
									Stdout   string
									ExitCode int
									ErrorMsg string
									Stderr   string
								}
							}
						}
					}
					// The CLI can print repository-migration diagnostics before JSON.
					start := strings.Index(string(desc), `{"Job":`)
					if start < 0 {
						e = fmt.Errorf("missing job JSON: %s", desc)
						break
					}
					e = json.Unmarshal(desc[start:], &d)
					if e != nil {
						break
					}
					if d.Job.State.StateType == "Failed" {
						e = fmt.Errorf("Bacalhau job failed: inspect %s", job)
						break
					}
					if d.Job.State.StateType == "Completed" {
						if len(d.Executions.Items) == 0 {
							e = fmt.Errorf("missing execution")
							break
						}
						x := d.Executions.Items[len(d.Executions.Items)-1]
						record["execution_id"] = x.ID
						record["node_id"] = x.NodeID
						if x.RunOutput.ExitCode != 0 {
							e = fmt.Errorf("worker exit %d: %s %s", x.RunOutput.ExitCode, x.RunOutput.ErrorMsg, x.RunOutput.Stderr)
						} else {
							result = []byte(x.RunOutput.Stdout)
						}
						break
					}
					if ctx.Err() != nil {
						e = ctx.Err()
						break
					}
					time.Sleep(200 * time.Millisecond)
				}
			}
		}
	} else {
		e = fmt.Errorf("choose native or bacalhau")
	}
	record["finished"] = time.Now().UTC()
	record["duration_ms"] = time.Since(started).Milliseconds()
	if e == nil {
		var v ufdr.Result
		e = json.Unmarshal(result, &v)
		if e == nil {
			record["result"] = v
			record["result_sha256"] = digest(result)
			record["original_xml"] = string(input)
		}
	}
	if e != nil {
		record["error"] = e.Error()
	}
	b, err := json.MarshalIndent(record, "", "  ")
	if err == nil {
		err = os.WriteFile(filepath.Join(evidence, attempt+".json"), b, 0600)
	}
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Write(b)
}
func main() {
	dir, e := os.UserConfigDir()
	if e != nil {
		log.Fatal(e)
	}
	s := &workbench{root: filepath.Join(dir, "VIII-PoC")}
	if e = os.MkdirAll(s.root, 0700); e != nil {
		log.Fatal(e)
	}
	app := application.New(application.Options{Name: "VIII synthetic PoC", Assets: application.AssetOptions{Handler: s}})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "VIII — synthetic evidence PoC", URL: "/", Width: 1000, Height: 700})
	if e = app.Run(); e != nil {
		log.Fatal(e)
	}
}
