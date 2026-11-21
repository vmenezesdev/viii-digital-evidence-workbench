#!/usr/bin/env python3
"""Adversarial cases at batch scale (BATCH-RESULTS.md).

  python batch_adversarial.py --fixtures DIR --pdq pdq-native.exe --flpdf flpdf-native.exe --out DIR

DIR holds adversarial.py's fixtures built from a 1,000-page original
(`adversarial.make`). Each case is split into 40-page chunks by both native
workers. Recorded per case and tool: exit status, the worker's error (one
line), manifest diagnostics, total and maximum chunk size, and the Poppler
comparison of the first and last chunk (batch_validate.check_set with the
original fixture as oracle: same content is expected for every readable case
except the incremental update, whose oracle is its own latest revision).
"""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import sys

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import batch_validate  # noqa: E402

CASES = ["shared-resources", "object-streams", "incremental", "broken-xref", "truncated", "encrypted-owner",
         "encrypted-user"]


def run(tool, exe, pdf, out, size, pages):
    if tool == "pdq":
        ack = out.with_suffix(".ack")
        ack.write_text("accepted")
        sha = hashlib.sha256(pdf.read_bytes()).hexdigest()
        cmd = [exe, str(pdf), "1", str(pages), str(out), str(ack), sha, str(size)]
    else:
        cmd = [exe, str(pdf), "--chunk-size", str(size), "--out-dir", str(out)]
    r = subprocess.run(cmd, capture_output=True, text=True, timeout=600)
    # Record the file name only, never local absolute paths.
    return r.returncode, (r.stderr or "").replace(str(pdf), pdf.name).strip()[:300]


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--fixtures", required=True, type=Path)
    p.add_argument("--pdq", required=True)
    p.add_argument("--flpdf", required=True)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--size", type=int, default=40)
    p.add_argument("--pages", type=int, default=1000, help="page count of the fixtures (pdq contract)")
    a = p.parse_args()
    a.out.mkdir(parents=True, exist_ok=False)
    rows = []
    for case in CASES:
        pdf = a.fixtures / f"{case}.pdf"
        for tool, exe in (("pdq", a.pdq), ("flpdf", a.flpdf)):
            out = a.out / f"{case}-{tool}"
            code, err = run(tool, exe, pdf, out, a.size, a.pages)
            row = {"case": case, "tool": tool, "exit": code, "error": err}
            if code == 0 and (out / "manifest.json").exists():
                m = json.loads((out / "manifest.json").read_text(encoding="utf-8"))
                sizes = [c["chunk_bytes"] for c in m["chunks"]]
                row |= {"chunks": len(sizes), "total_bytes": sum(sizes), "max_chunk_bytes": max(sizes),
                        "diagnostics": m.get("diagnostics")}
                # Oracle: the case file itself as rendered by Poppler (for incremental,
                # Poppler reads the latest revision, which is the intended content).
                rep, _ = batch_validate.check_set(pdf, f"{case}-{tool}", out, "first,last", False)
                row |= {"content_equal_first_last": rep["all_compared_equal"], "qpdf_check_failures":
                        len(rep["qpdf_check_failures"]), "set_verified": rep["set_verified"]}
            rows.append(row)
            print(json.dumps({k: row.get(k) for k in ("case", "tool", "exit", "chunks", "total_bytes", "max_chunk_bytes",
                                                      "content_equal_first_last", "diagnostics", "error")})[:400], flush=True)
    (a.out / "adversarial-batch.json").write_text(json.dumps(rows, indent=1), encoding="utf-8")


if __name__ == "__main__":
    main()
