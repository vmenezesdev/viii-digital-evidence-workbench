// Development-only staging for synthetic materials.
package main

import (
	"encoding/base64"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func main() {
	spike := flag.Bool("spike", false, "serve the isolated experiment artifacts from the current directory")
	traces := flag.Bool("traces", false, "capture local benchmark OTLP protobuf requests")
	flag.Parse()
	log.SetFlags(log.LstdFlags | log.LUTC | log.Lmicroseconds)
	mux := http.NewServeMux()
	address, prefix := "127.0.0.1:28080", ".poc-runtime/"
	files := []string{"worker.wasm"}
	if *spike {
		address, prefix = "127.0.0.1:38080", ""
		files = append(files, "bench-worker.wasm", "input-10240.bin", "input-102400.bin", "input-1048576.bin")
	}
	for _, file := range files {
		mux.HandleFunc("GET /"+file, func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			http.ServeFile(w, r, prefix+file)
			log.Printf("%s %s %s serve_ns=%d", r.RemoteAddr, r.Method, r.URL.Path, time.Since(start).Nanoseconds())
		})
	}
	if *spike {
		mux.HandleFunc("GET /evidence/{file}", func(w http.ResponseWriter, r *http.Request) {
			file := r.PathValue("file")
			attempt := strings.TrimSuffix(file, ".xml")
			if len(attempt) != 26 || attempt == file || strings.Trim(attempt, "abcdefghijklmnopqrstuvwxyz234567") != "" {
				http.NotFound(w, r)
				return
			}
			http.ServeFile(w, r, filepath.Join("staged", file))
			log.Printf("%s %s staged evidence", r.RemoteAddr, r.Method)
		})
	}
	if *spike && *traces {
		file, err := os.OpenFile("otlp-traces.base64", os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
		if err != nil {
			log.Fatal(err)
		}
		mux.HandleFunc("POST /v1/traces", func(w http.ResponseWriter, r *http.Request) {
			body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if _, err := file.WriteString(base64.StdEncoding.EncodeToString(body) + "\n"); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/x-protobuf")
			w.WriteHeader(http.StatusOK)
		})
	}
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second}
	log.Fatal(server.ListenAndServe())
}
