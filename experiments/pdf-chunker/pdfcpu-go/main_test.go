package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// syntheticPDF writes a minimal, valid n-page PDF whose page i says "PHYSICAL i".
func syntheticPDF(t *testing.T, path string, n int) {
	t.Helper()
	var b bytes.Buffer
	offsets := map[int]int{}
	emit := func(id int, body string) {
		offsets[id] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", id, body)
	}
	b.WriteString("%PDF-1.7\n")
	kids := []string{}
	next := 4
	for i := 1; i <= n; i++ {
		page, content := next, next+1
		next += 2
		kids = append(kids, fmt.Sprintf("%d 0 R", page))
		stream := fmt.Sprintf("BT /F1 12 Tf 40 700 Td (PHYSICAL %d) Tj ET", i)
		emit(content, fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(stream), stream))
		emit(page, fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 300 300] /Contents %d 0 R /Resources << /Font << /F1 3 0 R >> >> >>", content))
	}
	emit(1, "<< /Type /Catalog /Pages 2 0 R >>")
	emit(2, fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", n, strings.Join(kids, " ")))
	emit(3, "<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>")
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", next)
	for id := 1; id < next; id++ {
		fmt.Fprintf(&b, "%010d 00000 n \n", offsets[id])
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", next, xref)
	if err := os.WriteFile(path, b.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestParseArgs(t *testing.T) {
	sha := strings.Repeat("a", 64)
	if _, err := parseArgs([]string{"in", "3", "5", "out", "ack", sha}); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]string{
		{"in", "0", "5", "out", "ack"},
		{"in", "6", "5", "out", "ack"},
		{"in", "1", "30001", "out", "ack"},
		{"in", "x", "5", "out", "ack"},
		{"in", "1", "5", "out", "ack", "ABC"},
		{"in", "1", "5", "out", "ack", strings.Repeat("A", 64)},
		{"in", "1", "5", "out"},
	} {
		if _, err := parseArgs(bad); err == nil {
			t.Errorf("accepted invalid args %v", bad)
		}
	}
}

func TestPageMappingIsPhysical(t *testing.T) {
	got := pageMapping(29961, 30000)
	if len(got) != 40 || got[0] != (pageMap{1, 29961}) || got[38] != (pageMap{39, 29999}) || got[39] != (pageMap{40, 30000}) {
		t.Fatalf("bad mapping: first=%v len=%d", got[0], len(got))
	}
}

func TestChunkExtractsExactRangeAndRecordsProvenance(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "original.pdf")
	syntheticPDF(t, in, 7)
	before, _, _ := digest(in)
	out := filepath.Join(dir, "out")
	m, err := chunk(context.Background(), request{input: in, outDir: out, first: 3, last: 5, declaredSHA: before})
	if err != nil {
		t.Fatal(err)
	}
	if m.ChunkPageCount != 3 || m.PhysicalFirst != 3 || m.PhysicalLast != 5 || m.SourceSHA256Declared != before {
		t.Fatalf("bad manifest %+v", m)
	}
	if n, err := api.PageCountFile(context.Background(), filepath.Join(out, chunkName)); err != nil || n != 3 {
		t.Fatalf("derivative pages=%d err=%v", n, err)
	}
	if after, _, _ := digest(in); after != before {
		t.Fatal("original mutated")
	}
	if h, _, _ := digest(filepath.Join(out, chunkName)); h != m.ChunkSHA256 {
		t.Fatal("manifest hash differs from file")
	}
	// Idempotency guard: a second attempt into the same directory must not overwrite.
	if _, err := chunk(context.Background(), request{input: in, outDir: out, first: 3, last: 5}); err == nil {
		t.Fatal("overwrote an existing chunk")
	}
}

// Regression: in WASI there is no writable config/temp dir; pdfcpu must not try to create one.
func TestChunkDoesNotCreateConfigDirectory(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"HOME", "XDG_CONFIG_HOME", "AppData", "TMPDIR", "TEMP", "TMP"} {
		t.Setenv(k, home)
	}
	in := filepath.Join(dir, "original.pdf")
	syntheticPDF(t, in, 3)
	if _, err := chunk(context.Background(), request{input: in, outDir: filepath.Join(dir, "out"), first: 2, last: 2}); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("pdfcpu wrote configuration state: %v", entries)
	}
}

func TestChunkRejectsRangeBeyondDocument(t *testing.T) {
	dir := t.TempDir()
	in := filepath.Join(dir, "original.pdf")
	syntheticPDF(t, in, 4)
	out := filepath.Join(dir, "out")
	if _, err := chunk(context.Background(), request{input: in, outDir: out, first: 3, last: 6}); err == nil {
		t.Fatal("range beyond document produced a chunk")
	}
	for _, name := range []string{chunkName, manifestName, readyName} {
		if _, err := os.Stat(filepath.Join(out, name)); !os.IsNotExist(err) {
			t.Errorf("%s exists after failed chunk", name)
		}
	}
}

// Regression: Bacalhau's /out creates files on stat, so pre-existing outputs
// must be detected by listing, and a stray empty file must still block reuse.
func TestExistingOutputsUsesListing(t *testing.T) {
	dir := t.TempDir()
	if found, err := existingOutputs(dir); err != nil || len(found) != 0 {
		t.Fatalf("empty dir: %v %v", found, err)
	}
	if err := os.WriteFile(filepath.Join(dir, readyName), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if found, _ := existingOutputs(dir); len(found) != 1 || found[0] != readyName {
		t.Fatalf("stray ready not detected: %v", found)
	}
}

func TestWaitAck(t *testing.T) {
	dir := t.TempDir()
	ack := filepath.Join(dir, "accepted")
	if err := waitAck(ack, 30*time.Millisecond, 5*time.Millisecond); err == nil {
		t.Fatal("missing acknowledgement accepted")
	}
	go func() { time.Sleep(20 * time.Millisecond); _ = os.WriteFile(ack, []byte("ok"), 0o600) }()
	if err := waitAck(ack, 2*time.Second, 5*time.Millisecond); err != nil {
		t.Fatal(err)
	}
}
