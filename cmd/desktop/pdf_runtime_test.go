package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPDFRuntimePreservesNodeIdentityAfterProfileMove(t *testing.T) {
	old := &pdfRuntime{Root: t.TempDir()}
	node := old.node("wasi")
	moved := &pdfRuntime{Root: t.TempDir()}
	if err := os.MkdirAll(filepath.Join(moved.Root, "runtime"), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(map[string]string{"node": node})
	if err := os.WriteFile(filepath.Join(moved.Root, "runtime", "owned.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if moved.node("wasi") != node || moved.node("docker") != strings.TrimSuffix(node, "-wasi")+"-docker" {
		t.Fatal("moved profile changed its authorized local node")
	}
}

func TestPDFRuntimeIgnoresInvalidPersistedNodeIdentity(t *testing.T) {
	for _, node := range []string{"foreign-wasi", "viii-pdf-000000000000-docker", "viii-pdf-invalidnode-wasi"} {
		r := &pdfRuntime{Root: t.TempDir()}
		if err := os.MkdirAll(filepath.Join(r.Root, "runtime"), 0700); err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(map[string]string{"node": node})
		if err := os.WriteFile(filepath.Join(r.Root, "runtime", "owned.json"), data, 0600); err != nil {
			t.Fatal(err)
		}
		want := "viii-pdf-" + digest([]byte(r.Root))[:12] + "-wasi"
		if got := r.node("wasi"); got != want {
			t.Fatal("trusted invalid node", node, got)
		}
	}
}
