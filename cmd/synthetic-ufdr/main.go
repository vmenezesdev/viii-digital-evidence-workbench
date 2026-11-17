package main

import (
	"flag"
	"log"
	"os"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/ufdr"
)

func main() {
	count := flag.Int("messages", 32, "number of fictional messages")
	flag.Parse()
	if err := ufdr.WriteSynthetic(os.Stdout, *count); err != nil {
		log.Fatal(err)
	}
}
