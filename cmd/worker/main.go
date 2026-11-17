// Worker contract v0: one XML file path argument; normalized JSON on stdout.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/ufdr"
	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/wasijob"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: worker input.xml")
	}
	options := flag.NewFlagSet("worker", flag.ContinueOnError)
	expected := options.String("input-sha256", "", "expected staged input digest")
	compress := options.Bool("gzip", false, "return gzip/base64 in the bounded stdout field")
	delay := options.Int("delay-seconds", 0, "pause after normalization for failure injection")
	if err := options.Parse(args[1:]); err != nil {
		return err
	}
	if options.NArg() != 0 || *delay < 0 || *delay > 180 {
		return fmt.Errorf("invalid worker options")
	}
	input, err := os.ReadFile(args[0])
	if err != nil {
		return err
	}
	inputHash := wasijob.Digest(input)
	if *expected != "" && inputHash != *expected {
		return fmt.Errorf("staged input digest mismatch")
	}
	result, err := ufdr.Normalize(input)
	if err != nil {
		return err
	}
	if *delay > 0 {
		fmt.Fprintf(os.Stderr, "VIII_NORMALIZED sha256=%s messages=%d\n", inputHash, len(result.Messages))
		time.Sleep(time.Duration(*delay) * time.Second)
	}
	output, err := json.Marshal(result)
	if err != nil {
		return err
	}
	if *compress {
		encoded, err := wasijob.EncodeResult(output)
		if err != nil {
			return err
		}
		output = []byte(encoded)
	}
	_, err = io.WriteString(os.Stdout, string(output)+"\n")
	return err
}
