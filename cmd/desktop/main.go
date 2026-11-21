// VIII development PoC. Synthetic inputs only.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"flag"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/*
var assets embed.FS

func main() {
	policy := flag.String("placement", "", "trusted development placement configuration (JSON)")
	dataDir := flag.String("data-dir", "", "local evidence and execution records")
	bundle := flag.String("pdf-bundle", "", "installed PDF runtime directory")
	listen := flag.String("http", "", "loopback HTTP listener for validation")
	inference := flag.String("inference", "http://127.0.0.1:48081", "local llama-server endpoint")
	pdfJobs := flag.Int("pdf-jobs", defaultPDFJobs, "maximum simultaneous PDF page jobs on chunks")
	flag.Parse()
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	s := &workbench{root: filepath.Join(dir, "VIII")}
	if *dataDir != "" {
		s.root = *dataDir
	}
	s.root, err = filepath.Abs(s.root)
	if err != nil {
		log.Fatal(err)
	}
	if *bundle == "" {
		exe, e := os.Executable()
		if e != nil {
			log.Fatal(e)
		}
		*bundle = filepath.Join(filepath.Dir(exe), "runtime")
	}
	*bundle, err = filepath.Abs(*bundle)
	if err != nil {
		log.Fatal(err)
	}
	s.pdf = &pdfService{s: s, runtime: &pdfRuntime{Root: s.root, Bundle: *bundle, Inference: *inference}, Jobs: *pdfJobs}
	if *policy != "" {
		data, err := os.ReadFile(*policy)
		if err != nil {
			log.Fatal(err)
		}
		if err = json.Unmarshal(data, &s.placement); err != nil {
			log.Fatal(err)
		}
	}
	if err = os.MkdirAll(s.root, 0700); err != nil {
		log.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go s.pdf.run(ctx)
	if *listen != "" {
		host, _, err := net.SplitHostPort(*listen)
		if err != nil || host != "127.0.0.1" {
			log.Fatal("validation listener must use 127.0.0.1")
		}
		log.Fatal(http.ListenAndServe(*listen, s))
		return
	}
	app := application.New(application.Options{Name: "VIII", Windows: application.WindowsOptions{WebviewUserDataPath: filepath.Join(s.root, "webview")}, Assets: application.AssetOptions{Handler: s}})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "VIII — PDFs", URL: "/pdf.html", Width: 1200, Height: 850})
	if err = app.Run(); err != nil {
		log.Fatal(err)
	}
}

var digest = wasijob.Digest
