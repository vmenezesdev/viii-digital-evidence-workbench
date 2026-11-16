// Development-only staging for synthetic materials.
package main

import (
	"flag"
	"log"
	"net/http"
	"time"
)

func main() {
	spike := flag.Bool("spike", false, "serve the isolated experiment artifacts from the current directory")
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
	server := &http.Server{Addr: address, Handler: mux, ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 20 * time.Second}
	log.Fatal(server.ListenAndServe())
}
