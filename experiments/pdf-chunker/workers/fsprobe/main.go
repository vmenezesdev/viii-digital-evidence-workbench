// fsprobe characterises file-system semantics seen by a WASI guest.
package main

import (
	"bytes"
	"fmt"
	"os"
)

// All output goes to one buffer, emitted with a single write: Bacalhau v1.9.5
// RunOutput capture was observed to keep only part of multi-write output.
var out bytes.Buffer

func stat(p string) {
	fi, err := os.Stat(p)
	if err != nil {
		fmt.Fprintf(&out, "stat %-26s err=%v notexist=%v\n", p, err, os.IsNotExist(err))
		return
	}
	fmt.Fprintf(&out, "stat %-26s ok dir=%v size=%d mode=%v\n", p, fi.IsDir(), fi.Size(), fi.Mode())
}

func main() {
	for _, p := range []string{"/", "/out", "/out/missing.pdf", "/control", "/control/missing.ack", "/original.pdf", "/missing"} {
		stat(p)
	}
	fmt.Fprintln(&out, "mkdirall /out:", os.MkdirAll("/out", 0o700))
	fmt.Fprintln(&out, "write /out/a.txt:", os.WriteFile("/out/a.txt", []byte("a"), 0o600))
	stat("/out/a.txt")
	stat("/out/b.txt")
	fmt.Fprintln(&out, "rename a->c:", os.Rename("/out/a.txt", "/out/c.txt"))
	stat("/out/a.txt")
	stat("/out/c.txt")
	entries, err := os.ReadDir("/out")
	fmt.Fprintln(&out, "readdir /out:", len(entries), err)
	for _, e := range entries {
		fmt.Fprintln(&out, "  entry", e.Name())
	}
	f, err := os.OpenFile("/out/excl.bin", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	fmt.Fprintln(&out, "open O_EXCL new /out/excl.bin:", err)
	if f != nil {
		_, werr := f.Write([]byte("xyz"))
		fmt.Fprintln(&out, "write+close:", werr, f.Close())
	}
	_, err = os.OpenFile("/out/excl.bin", os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	fmt.Fprintln(&out, "open O_EXCL existing /out/excl.bin:", err)
	fmt.Fprintln(&out, "remove /out/c.txt:", os.Remove("/out/c.txt"))
	entries, _ = os.ReadDir("/out")
	for _, e := range entries {
		info, _ := e.Info()
		fmt.Fprintln(&out, "  after", e.Name(), info.Size())
	}
	os.Stdout.Write(out.Bytes())
}
