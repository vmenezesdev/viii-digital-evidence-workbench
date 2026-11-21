package main

import (
	"context"
	"crypto/rand"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

const bacalhauRevision = "3a6a73aa6529755ac2c9d96a771f0566ff3ee5da"
const bacalhauImage = "ghcr.io/bacalhau-project/bacalhau@sha256:903c4052a4522720adf43d428bdbb267cb24dec5039a759e27dffbcbbd68d878"

type pdfRuntime struct {
	Root, Bundle, Inference                     string
	Client                                      wasijob.Client
	Worker, WorkerSHA, ModelDir, Image, ImageID string
	// Chunker is the bundled flpdf WASI module (ADR-0003); empty in older bundles.
	Chunker, ChunkerSHA                        string
	apiPort, natsPort                          int
	name, token, volume, volumeRoot, container string
	verified                                   bool
	stagedOriginal                             string
}

func (r *pdfRuntime) node(engine string) string {
	if r.name == "" {
		// Bacalhau keeps its node name when a data directory is moved.
		var owned struct{ Node string }
		data, err := os.ReadFile(filepath.Join(r.Root, "runtime", "owned.json"))
		if err == nil && json.Unmarshal(data, &owned) == nil && strings.HasPrefix(owned.Node, "viii-pdf-") && strings.HasSuffix(owned.Node, "-wasi") {
			id := strings.TrimSuffix(strings.TrimPrefix(owned.Node, "viii-pdf-"), "-wasi")
			if _, err := hex.DecodeString(id); err == nil && len(id) == 12 && id == strings.ToLower(id) {
				r.name = "viii-pdf-" + id
			}
		}
	}
	if r.name == "" {
		r.name = "viii-pdf-" + digest([]byte(r.Root))[:12]
	}
	return r.name + "-" + engine
}

func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func (r *pdfRuntime) ensure(ctx context.Context) error {
	if !r.verified {
		data, err := os.ReadFile(filepath.Join(r.Bundle, "manifest.json"))
		if err != nil {
			return errors.New("Instalação incompleta: pacote runtime/manifest.json não encontrado")
		}
		var manifest struct{ WorkerSHA256, ModelSHA256, BacalhauSHA256, DockerImage, ChunkerSHA256 string }
		if err := json.Unmarshal(data, &manifest); err != nil {
			return err
		}
		r.Worker = filepath.Join(r.Bundle, "pdf-worker.wasm")
		r.ModelDir = filepath.Join(r.Bundle, "models")
		for path, want := range map[string]string{r.Worker: manifest.WorkerSHA256, filepath.Join(r.ModelDir, "por.traineddata"): manifest.ModelSHA256, filepath.Join(r.Bundle, "bacalhau.exe"): manifest.BacalhauSHA256} {
			got, err := fileDigest(path)
			if err != nil || want == "" || !strings.EqualFold(got, want) {
				return fmt.Errorf("Bundled artifact integrity failure: %s", filepath.Base(path))
			}
		}
		info, err := buildinfo.ReadFile(filepath.Join(r.Bundle, "bacalhau.exe"))
		if err != nil {
			return err
		}
		revision := ""
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" {
				revision = setting.Value
			}
		}
		if info.Main.Version != "v1.9.5" || revision != bacalhauRevision {
			return errors.New("Bacalhau must be pinned to v1.9.5 / " + bacalhauRevision)
		}
		r.WorkerSHA = strings.ToLower(manifest.WorkerSHA256)
		if manifest.ChunkerSHA256 != "" {
			chunker := filepath.Join(r.Bundle, "pdf-chunker.wasm")
			got, err := fileDigest(chunker)
			if err != nil || !strings.EqualFold(got, manifest.ChunkerSHA256) {
				return errors.New("Bundled artifact integrity failure: pdf-chunker.wasm")
			}
			r.Chunker, r.ChunkerSHA = chunker, strings.ToLower(manifest.ChunkerSHA256)
		}
		r.Image = manifest.DockerImage
		if r.Image == "" {
			r.Image = "viii-pdf-worker:local"
		}
		dir := filepath.Join(r.Root, "runtime")
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
		config := filepath.Join(dir, "ports.json")
		var ports struct {
			API, NATS int
			Token     string
		}
		data, err = os.ReadFile(config)
		if os.IsNotExist(err) {
			ports.API, err = freePort()
			if err != nil {
				return err
			}
			ports.NATS, err = freePort()
			if err != nil {
				return err
			}
			ports.Token = rand.Text()
			data, _ = json.Marshal(ports)
			if err := atomicWrite(config, data); err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else if err := json.Unmarshal(data, &ports); err != nil {
			return err
		}
		if ports.API < 1 || ports.NATS < 1 || ports.Token == "" {
			return errors.New("Invalid persisted runtime ports")
		}
		r.apiPort = ports.API
		r.natsPort = ports.NATS
		r.token = ports.Token
		r.Client = wasijob.Client{Base: fmt.Sprintf("http://127.0.0.1:%d", r.apiPort)}
		r.verified = true
	}
	ready, err := r.hasNode(ctx, r.node("wasi"))
	if ready {
		return nil
	}
	if err == nil {
		return errors.New("A different runtime is using the configured API port")
	}
	for _, port := range []int{r.apiPort, r.natsPort} {
		l, e := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if e != nil {
			return fmt.Errorf("Runtime port unavailable: %w", e)
		}
		l.Close()
	}
	dir := filepath.Join(r.Root, "runtime")
	allow := []string{filepath.ToSlash(r.Worker) + ":ro", filepath.ToSlash(r.ModelDir) + ":ro", filepath.ToSlash(filepath.Join(r.Root, "originals")) + "/**:ro", filepath.ToSlash(filepath.Join(r.Root, "control")) + "/**:ro", filepath.ToSlash(filepath.Join(r.Root, "chunks")) + "/**:ro"}
	if r.Chunker != "" {
		allow = append(allow, filepath.ToSlash(r.Chunker)+":ro")
	}
	config := map[string]any{"DisableAnalytics": true, "API": map[string]any{"Host": "127.0.0.1", "Port": r.apiPort}, "Orchestrator": map[string]any{"Enabled": true, "Host": "127.0.0.1", "Port": r.natsPort, "Auth": map[string]string{"Token": r.token}, "NodeManager": map[string]bool{"ManualApproval": true}}, "Compute": map[string]any{"Enabled": true, "Orchestrators": []string{fmt.Sprintf("nats://127.0.0.1:%d", r.natsPort)}, "Auth": map[string]string{"Token": r.token}, "AllowListedLocalPaths": allow}, "Engines": map[string]any{"Disabled": []string{"docker"}}, "Publishers": map[string]any{"Disabled": []string{"local"}}}
	data, _ := json.MarshalIndent(config, "", "  ")
	if err := atomicWrite(filepath.Join(dir, "node.yaml"), data); err != nil {
		return err
	}
	stdout, err := os.OpenFile(filepath.Join(dir, "node.stdout.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stdout.Close()
	stderr, err := os.OpenFile(filepath.Join(dir, "node.stderr.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer stderr.Close()
	cmd := exec.Command(filepath.Join(r.Bundle, "bacalhau.exe"), "serve", "--name", r.node("wasi"), "--data-dir", "node", "-c", "node.yaml")
	// Bound retained Go/Wasm heaps between ranges on the measured 16 GB workstation.
	cmd.Env = append(os.Environ(), "GOMEMLIMIT=3GiB")
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	hideProcess(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	owned, _ := json.Marshal(map[string]any{"pid": cmd.Process.Pid, "started": time.Now().UTC(), "node": r.node("wasi"), "api": r.Client.Base})
	if err := atomicWrite(filepath.Join(dir, "owned.json"), owned); err != nil {
		return err
	}
	cmd.Process.Release()
	return r.waitNode(ctx, r.node("wasi"))
}

func (r *pdfRuntime) hasNode(ctx context.Context, id string) (bool, error) {
	var result struct {
		Nodes []struct {
			Info       struct{ NodeID string }
			Membership string
		}
	}
	if err := r.Client.Request(ctx, "GET", "/nodes", nil, &result); err != nil {
		return false, err
	}
	for _, node := range result.Nodes {
		if node.Info.NodeID == id {
			if node.Membership == "PENDING" {
				if err := r.Client.Request(ctx, "PUT", "/nodes/"+id, map[string]string{"Action": "approve"}, nil); err != nil {
					return false, err
				}
			}
			return true, nil
		}
	}
	return false, nil
}

func (r *pdfRuntime) waitNode(ctx context.Context, id string) error {
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		if ready, _ := r.hasNode(ctx, id); ready {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("Runtime startup pending: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func dockerCommand(ctx context.Context, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "docker", args...)
	hideProcess(cmd)
	data, err := cmd.CombinedOutput()
	if err != nil {
		if len(data) > 2048 {
			data = data[:2048]
		}
		return "", fmt.Errorf("docker %s: %w: %s", args[0], err, data)
	}
	return strings.TrimSpace(string(data)), nil
}

func (r *pdfRuntime) ensureDocker(ctx context.Context) error {
	if err := r.ensure(ctx); err != nil {
		return err
	}
	imageID, err := dockerCommand(ctx, "image", "inspect", r.Image, "--format", "{{.Id}}")
	if err != nil {
		return err
	}
	r.ImageID = imageID
	r.volume = r.name + "-data"
	r.container = r.name + "-compute"
	if _, err := dockerCommand(ctx, "volume", "create", r.volume); err != nil {
		return err
	}
	r.volumeRoot, err = dockerCommand(ctx, "volume", "inspect", r.volume, "--format", "{{.Mountpoint}}")
	if err != nil {
		return err
	}
	if running, e := dockerCommand(ctx, "inspect", r.container, "--format", "{{.State.Running}}"); e == nil {
		if running != "true" {
			if _, err := dockerCommand(ctx, "start", r.container); err != nil {
				return err
			}
		}
		return r.waitNode(ctx, r.node("docker"))
	}
	if _, err := dockerCommand(ctx, "run", "--rm", "--mount", "type=volume,source="+r.volume+",target=/data", "alpine:3", "mkdir", "-p", "/data/models", "/data/control", "/data/out"); err != nil {
		return err
	}
	config := map[string]any{"DisableAnalytics": true, "API": map[string]any{"Host": "127.0.0.1", "Port": r.apiPort}, "Compute": map[string]any{"Enabled": true, "Orchestrators": []string{fmt.Sprintf("nats://host.docker.internal:%d", r.natsPort)}, "Auth": map[string]string{"Token": r.token}, "AllowListedLocalPaths": []string{r.volumeRoot + "/original.pdf:ro", r.volumeRoot + "/models:ro", r.volumeRoot + "/control/**:ro", r.volumeRoot + "/out/**:rw"}}, "Publishers": map[string]any{"Disabled": []string{"local"}}}
	data, _ := json.MarshalIndent(config, "", "  ")
	path := filepath.Join(r.Root, "runtime", "docker.yaml")
	if err := atomicWrite(path, data); err != nil {
		return err
	}
	if _, err := dockerCommand(ctx, "run", "-d", "--name", r.container, "--mount", "type=volume,source="+r.volume+",target="+r.volumeRoot, "--mount", "type=bind,source="+path+",target=/viii.yaml,readonly", "--mount", "type=bind,source=/var/run/docker.sock,target=/var/run/docker.sock", bacalhauImage, "serve", "--name", r.node("docker"), "--data-dir", r.volumeRoot+"/node", "-c", "/viii.yaml"); err != nil {
		return err
	}
	owned, _ := json.Marshal(map[string]string{"container": r.container, "volume": r.volume, "node": r.node("docker"), "image": bacalhauImage})
	if err := atomicWrite(filepath.Join(r.Root, "runtime", "docker-owned.json"), owned); err != nil {
		return err
	}
	if _, err := dockerCommand(ctx, "cp", filepath.Join(r.ModelDir, "por.traineddata"), r.container+":"+r.volumeRoot+"/models/por.traineddata"); err != nil {
		return err
	}
	return r.waitNode(ctx, r.node("docker"))
}

func localMount(source, target string, writable bool) map[string]any {
	return map[string]any{"Source": map[string]any{"Type": "local", "Params": map[string]any{"SourcePath": filepath.ToSlash(source), "ReadWrite": writable}}, "Target": target}
}

// spec builds a page job. input is the original or, when chunk is set, one
// verified chunk; first and last are pages of that input.
func (r *pdfRuntime) spec(ctx context.Context, input, control, id, node, phase string, first, last int, chunk bool) (map[string]any, error) {
	args := []string{"/original.pdf", fmt.Sprint(first), fmt.Sprint(last), "/out", "/control/accepted", phase, "/model"}
	// A job on a 40-page chunk peaked at ~276 MiB; reserving 2 GB each let only
	// three jobs fit the Docker VM and Bacalhau rejected the rest.
	memory := "2gb"
	if chunk {
		memory = "1gb"
	}
	task := map[string]any{"Name": "main", "Resources": map[string]string{"CPU": "1", "Memory": memory}, "Timeouts": map[string]int{"TotalTimeout": 3600}, "Network": map[string]string{"Type": "none"}}
	if strings.HasSuffix(node, "-wasi") {
		actual, err := fileDigest(r.Worker)
		if err != nil || actual != r.WorkerSHA {
			return nil, errors.New("Worker integrity failure")
		}
		task["Engine"] = map[string]any{"Type": "wasm", "Params": map[string]any{"EntryModule": "/worker", "Entrypoint": "_start", "Parameters": args}}
		task["InputSources"] = []any{localMount(r.Worker, "/worker", false), localMount(input, "/original.pdf", false), localMount(r.ModelDir, "/model", false), localMount(control, "/control", false)}
		task["ResultPaths"] = []any{map[string]string{"Name": "out", "Path": "C:/out"}}
		task["Publisher"] = map[string]string{"Type": "noop"}
	} else {
		if _, err := dockerCommand(ctx, "run", "--rm", "--mount", "type=volume,source="+r.volume+",target=/data", "alpine:3", "mkdir", "-p", "/data/control/"+id, "/data/out/"+id); err != nil {
			return nil, err
		}
		staged := r.volumeRoot + "/original.pdf"
		if !chunk && r.stagedOriginal != input {
			if _, err := dockerCommand(ctx, "cp", input, r.container+":"+staged); err != nil {
				return nil, err
			}
			r.stagedOriginal = input
		}
		if _, err := dockerCommand(ctx, "cp", control+string(os.PathSeparator)+".", r.container+":"+r.volumeRoot+"/control/"+id); err != nil {
			return nil, err
		}
		if chunk {
			// Each chunk job gets its own staged copy, so concurrent jobs never share an input path.
			staged = r.volumeRoot + "/control/" + id + "/input.pdf"
			if _, err := dockerCommand(ctx, "cp", input, r.container+":"+staged); err != nil {
				return nil, err
			}
		}
		task["Engine"] = map[string]any{"Type": "docker", "Params": map[string]any{"Image": r.ImageID, "Entrypoint": []string{"/pdf-worker"}, "Parameters": args, "SkipImagePull": true}}
		task["InputSources"] = []any{localMount(staged, "/original.pdf", false), localMount(r.volumeRoot+"/models", "/model", false), localMount(r.volumeRoot+"/control/"+id, "/control", false), localMount(r.volumeRoot+"/out/"+id, "/out", true)}
	}
	return map[string]any{"Name": "viii-" + id, "Type": "batch", "Count": 1, "Constraints": []any{map[string]any{"Key": "name", "Operator": "in", "Values": []string{node}}}, "Tasks": []any{task}}, nil
}

// prepareSpec builds the flpdf batch job that splits the original into chunks
// (from physical page first; last == 0 means the whole document).
func (r *pdfRuntime) prepareSpec(original, control, id, node, sha string, size, first, last int) (map[string]any, error) {
	actual, err := fileDigest(r.Chunker)
	if err != nil || actual != r.ChunkerSHA {
		return nil, errors.New("Chunker integrity failure")
	}
	args := []string{"/original.pdf", "--chunk-size", fmt.Sprint(size), "--out-dir", "/out", "--ack", "/control/accepted", "--source-sha256", sha}
	if last > 0 {
		args = append(args, "--range", fmt.Sprintf("%d-%d", first, last))
	}
	task := map[string]any{"Name": "main", "Resources": map[string]string{"CPU": "1", "Memory": "1gb"}, "Timeouts": map[string]int{"TotalTimeout": 3600}, "Network": map[string]string{"Type": "none"},
		"Engine":       map[string]any{"Type": "wasm", "Params": map[string]any{"EntryModule": "/worker", "Entrypoint": "_start", "Parameters": args}},
		"InputSources": []any{localMount(r.Chunker, "/worker", false), localMount(original, "/original.pdf", false), localMount(control, "/control", false)},
		"ResultPaths":  []any{map[string]string{"Name": "out", "Path": "C:/out"}},
		"Publisher":    map[string]string{"Type": "noop"}}
	return map[string]any{"Name": "viii-" + id, "Type": "batch", "Count": 1, "Constraints": []any{map[string]any{"Key": "name", "Operator": "in", "Values": []string{node}}}, "Tasks": []any{task}}, nil
}

func (r *pdfRuntime) outputs(ctx context.Context, engine string, a *attemptRecord) (string, error) {
	if engine == "wasi" {
		return filepath.Join(r.Root, "runtime", "node", "compute", "executions", "output", a.ExecutionID, "results", "out"), nil
	}
	if err := r.ensureDocker(ctx); err != nil {
		return "", err
	}
	dir := filepath.Join(r.Root, "artifacts", a.Attempt)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	_, err := dockerCommand(ctx, "cp", r.container+":"+r.volumeRoot+"/out/"+a.Attempt+"/.", dir)
	return dir, err
}

func (r *pdfRuntime) acknowledge(ctx context.Context, engine string, a *attemptRecord) error {
	path := filepath.Join(a.StagingDir, "accepted")
	if _, err := os.Stat(path); err == nil && engine == "wasi" {
		return nil
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		if err := atomicWrite(path, []byte("accepted")); err != nil {
			return err
		}
	}
	if engine == "docker" {
		_, err := dockerCommand(ctx, "cp", path, r.container+":"+r.volumeRoot+"/control/"+a.Attempt+"/accepted")
		return err
	}
	return nil
}
