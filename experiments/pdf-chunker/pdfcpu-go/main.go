// Executable experimental PDF range chunker for Go WASI Preview 1.
// Contract: chunker original.pdf first last out-dir acknowledgement-file
// Do not use for evidentiary decisions until the Bacalhau gate has passed.
package main

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "encoding/json"
    "fmt"
    "io"
    "os"
    "path/filepath"
    "strconv"
    "time"

    "github.com/pdfcpu/pdfcpu/pkg/api"
)

func digest(path string) (string, error) {
    file, err := os.Open(path)
    if err != nil { return "", err }
    defer file.Close()
    hash := sha256.New()
    if _, err := io.Copy(hash, file); err != nil { return "", err }
    return hex.EncodeToString(hash.Sum(nil)), nil
}

func run() error {
    if len(os.Args) != 6 { return fmt.Errorf("usage: chunker input.pdf first last out-dir ack-file") }
    input, outputDir, ack := os.Args[1], os.Args[4], os.Args[5]
    first, err := strconv.Atoi(os.Args[2])
    if err != nil || first < 1 { return fmt.Errorf("invalid first page") }
    last, err := strconv.Atoi(os.Args[3])
    if err != nil || last < first { return fmt.Errorf("invalid last page") }
    if err := os.MkdirAll(outputDir, 0700); err != nil { return err }
    output := filepath.Join(outputDir, "chunk.pdf")
    // pdfcpu may still materialize many document objects; measure, do not presume bounded RAM.
    if err := api.TrimFile(context.Background(), input, output,
        []string{fmt.Sprintf("%d-%d", first, last)}, nil); err != nil { return err }
    hash, err := digest(output)
    if err != nil { return err }
    data, err := json.Marshal(map[string]any{
        "tool": "pdfcpu", "range_first": first, "range_last": last,
        "local_first": 1, "local_last": last - first + 1,
        "chunk_sha256": hash,
    })
    if err != nil { return err }
    if err := os.WriteFile(filepath.Join(outputDir, "info.json"), data, 0600); err != nil { return err }
    if err := os.WriteFile(filepath.Join(outputDir, "ready"), []byte("ready"), 0600); err != nil { return err }
    deadline := time.Now().Add(120 * time.Second)
    for {
        if _, err := os.Stat(ack); err == nil { return nil
        } else if !os.IsNotExist(err) { return err }
        if time.Now().After(deadline) { return fmt.Errorf("host acknowledgement timed out") }
        time.Sleep(100 * time.Millisecond)
    }
}

func main() {
    if err := run(); err != nil {
        fmt.Fprintln(os.Stderr, err)
        os.Exit(1)
    }
}
