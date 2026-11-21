package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPDFAnswerUsesRetrievedReferences(t *testing.T) {
	s := &workbench{root: t.TempDir()}
	id := strings.Repeat("a", 64)
	_, err := s.savePDFPage(id, pdfPage{Page: 11, Text: "JABUTICABA: pagamento previsto para 15 de março.", Source: "ocr", Status: "complete", Tool: "tesseract", Version: "5.5.0", Engine: "wasi", RunID: "execution-1"})
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		data, _ := json.Marshal(body)
		if !strings.Contains(string(data), "15 de março") {
			t.Error("missing retrieved evidence")
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"content":"O pagamento está previsto para 15 de março."}}]}`))
	}))
	defer backend.Close()
	answer, err := s.answerPDF(context.Background(), id, "JABUTICABA", "O que dizem os trechos?", backend.URL, 12)
	if err != nil || !answer.Partial || len(answer.Sources) != 1 || answer.Sources[0].Page != 11 || !strings.HasSuffix(answer.Sources[0].URL, "#page=11") {
		t.Fatalf("invalid cited answer: %+v %v", answer, err)
	}
	answer, err = s.answerPDF(context.Background(), id, "AUSENTE", "Onde aparece AUSENTE?", backend.URL, 12)
	if err != nil || !answer.Insufficient || calls != 1 {
		t.Fatalf("missing evidence should not call inference: %+v %v", answer, err)
	}
	if _, err := s.answerPDF(context.Background(), id, "JABUTICABA", "O que diz?", "https://external.invalid", 12); err == nil {
		t.Fatal("external inference endpoint accepted")
	}
}

func TestPDFRealLocalAnswer(t *testing.T) {
	dir := os.Getenv("VIII_PDF_PAGES_PROOF")
	backend := os.Getenv("VIII_PDF_INFERENCE")
	if dir == "" || backend == "" {
		t.Skip("set VIII_PDF_PAGES_PROOF and VIII_PDF_INFERENCE for real-model verification")
	}
	data, err := os.ReadFile(filepath.Join(dir, "summary.json"))
	if err != nil {
		t.Fatal(err)
	}
	var summary struct {
		InputSHA256 string `json:"input_sha256"`
	}
	if err := json.Unmarshal(data, &summary); err != nil {
		t.Fatal(err)
	}
	s := &workbench{root: filepath.Join(dir, "viii")}
	answer, err := s.answerPDF(context.Background(), strings.ToLower(summary.InputSHA256), "JABUTICABA", "Onde aparece JABUTICABA e qual é a data prevista para pagamento?", backend, 12)
	if err != nil || len(answer.Sources) != 1 || answer.Sources[0].Page != 11 || !strings.Contains(answer.Answer, "15") || !strings.Contains(strings.ToLower(answer.Answer), "março") {
		t.Fatalf("real local answer failed: %+v %v", answer, err)
	}
	data, err = json.MarshalIndent(answer, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := atomicWrite(filepath.Join(dir, "answer.json"), data); err != nil {
		t.Fatal(err)
	}
}
