// Implements a synthetic benchmark worker and the native measurement driver.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/vmenezesdev/viii-digital-evidence-workbench/internal/spikework"
)

func worker(path, count string) error {
	rounds, err := strconv.ParseUint(count, 10, 64)
	if err != nil {
		return err
	}
	read := time.Now()
	input, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	readNS := time.Since(read).Nanoseconds()
	result, err := spikework.Run(context.Background(), input, rounds)
	if err != nil {
		return err
	}
	result.ReadNS = readNS
	return json.NewEncoder(os.Stdout).Encode(result)
}

func main() {
	var err error
	switch {
	case len(os.Args) >= 2 && os.Args[1] == "measure":
		err = measure(os.Args[2:])
	case len(os.Args) == 3:
		err = worker(os.Args[1], os.Args[2])
	default:
		err = fmt.Errorf("usage: bench-worker INPUT ROUNDS | bench-worker measure [-short]")
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
