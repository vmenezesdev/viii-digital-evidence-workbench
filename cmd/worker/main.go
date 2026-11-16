// Worker contract v0: one XML file path argument; normalized JSON on stdout.
package main

import (
	"encoding/json"
	"fmt"
	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/ufdr"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: worker input.xml")
		os.Exit(2)
	}
	b, e := os.ReadFile(os.Args[1])
	if e == nil {
		var r ufdr.Result
		r, e = ufdr.Normalize(b)
		if e == nil {
			e = json.NewEncoder(os.Stdout).Encode(r)
		}
	}
	if e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
