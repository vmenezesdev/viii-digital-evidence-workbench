// Development-only staging server. It exposes only the synthetic worker binary.
package main

import (
	"log"
	"net/http"
)

func main() {
	http.HandleFunc("/worker.wasm", func(w http.ResponseWriter, r *http.Request) { http.ServeFile(w, r, ".poc-runtime/worker.wasm") })
	log.Fatal(http.ListenAndServe("127.0.0.1:28080", nil))
}
