// VIII development PoC. Synthetic inputs only.
package main

import (
	"embed"
	"encoding/json"
	"flag"
	"log"
	"os"
	"path/filepath"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed assets/*
var assets embed.FS

func main() {
	policy := flag.String("placement", "", "trusted development placement configuration (JSON)")
	flag.Parse()
	dir, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	s := &workbench{root: filepath.Join(dir, "VIII-PoC")}
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
	app := application.New(application.Options{Name: "VIII synthetic PoC", Assets: application.AssetOptions{Handler: s}})
	app.Window.NewWithOptions(application.WebviewWindowOptions{Title: "VIII — synthetic evidence PoC", URL: "/", Width: 1000, Height: 700})
	if err = app.Run(); err != nil {
		log.Fatal(err)
	}
}

var digest = wasijob.Digest
