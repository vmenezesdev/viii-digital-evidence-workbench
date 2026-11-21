#!/usr/bin/env python3
"""Compact the ALTERNATIVES.md raw runs into results/alternatives.json.

  python alt_summarize.py --runs .poc-runtime/alt --out results/alternatives.json

Inputs (all produced by the commands in ALTERNATIVES.md):
  native.jsonl, wasmtime.jsonl   alt_bench.py runs (one JSON per run)
  matrix/wasi-*/summary.json     run_matrix.py wasi (Bacalhau, one node per run)
  matrix/startup-*/summary.json  run_matrix.py startup (module exits at once)
  validate/*.json, adversarial/adversarial.json
Only observed values are written; failed or missing runs are listed as such.
"""
import argparse
from collections import defaultdict
import json
from pathlib import Path
import statistics

MB = 2**20


def load(p):
    try:
        return json.loads(Path(p).read_text(encoding="utf-8"))
    except (OSError, ValueError):
        return None


def mb(v):
    return None if v is None else round(v / MB, 1)


def span(values):
    values = [v for v in values if v is not None]
    if not values:
        return None
    return {"median": round(statistics.median(values), 3), "min": round(min(values), 3), "max": round(max(values), 3)}


def local_rows(path):
    groups = defaultdict(list)
    for line in Path(path).read_text(encoding="utf-8").splitlines() if Path(path).exists() else []:
        r = json.loads(line)
        groups[(r["candidate"], r["runtime"], r["fixture"], r["first"], r["last"])].append(r)
    rows = []
    for (cand, runtime, fixture, first, last), runs in sorted(groups.items()):
        ok = [r for r in runs if r["returncode"] == 0]
        g = [r["guest"] or {} for r in ok]
        rows.append({
            "candidate": cand, "runtime": runtime, "fixture": fixture, "first": first, "last": last,
            "runs": len(runs), "ok": len(ok),
            "elapsed_s": span([r["elapsed_seconds"] for r in ok]),
            "peak_working_set_mib": span([mb(r["exact"].get("peak_working_set_bytes")) for r in ok]),
            "peak_private_mib": span([mb(r["exact"].get("peak_private_bytes")) for r in ok]),
            "os_read_mib": span([mb(r["exact"].get("read_bytes")) for r in ok]),
            "page_faults": span([r["exact"].get("page_faults") for r in ok]),
            "guest_source_read_mib": span([mb(x.get("io", {}).get("source_bytes_read")) for x in g]),
            "guest_read_calls": span([x.get("io", {}).get("source_read_calls") for x in g]),
            "guest_host_seeks": span([x.get("io", {}).get("source_seeks") for x in g]),
            "guest_linear_memory_mib": span([mb(x.get("guest_linear_memory_final")) for x in g]),
            "guest_open_ms": span([x.get("open_ms") for x in g]),
            "guest_extract_ms": span([x.get("extract_ms") for x in g]),
            "guest_write_ms": span([x.get("write_ms") for x in g]),
            "chunk_bytes": g[0].get("chunk_bytes") if g else None,
            "chunk_sha256": sorted({r["chunk_sha256"] for r in ok}),
            "original_unchanged": all(r["original_unchanged"] for r in runs),
            "killed_by_memory_guard": any(r["killed_by_memory_guard"] for r in runs),
            "note": "first run of a module under Wasmtime includes its compilation cache fill" if runtime == "wasmtime" else None,
        })
    return rows


def bacalhau_rows(matrix):
    groups = defaultdict(list)
    for d in sorted(Path(matrix).glob("wasi-*")):
        s = load(d / "summary.json")
        name = d.name.rsplit("-r", 1)[0]
        groups[name].append((d, s))
    rows = []
    for name, runs in groups.items():
        ok, failed = [], []
        for d, s in runs:
            (ok if s and s.get("passed") else failed).append((d, s))
        manifests = []
        for d, _ in ok:
            m = next(d.glob("artifacts/**/manifest.json"), None)
            manifests.append(load(m) if m else {})
        rows.append({
            "run": name, "runs": len(runs), "passed": len(ok), "failed": [str(d.name) for d, _ in failed],
            "exit_codes": sorted({j.get("exit_code") for _, s in runs if s for j in s["jobs"]}),
            "node_peak_wset_minus_baseline_mib": span([mb(s["node_peak_wset_minus_baseline_rss"]) for _, s in ok]),
            "node_peak_private_minus_baseline_mib": span([mb(s["node_peak_private_minus_baseline"]) for _, s in ok]),
            "ready_s": span([s["jobs"][0].get("ready_s") for _, s in ok]),
            "node_read_mib": span([mb(s.get("node_read_bytes_during_jobs")) for _, s in ok]),
            "guest_source_read_mib": span([mb(m.get("io", {}).get("source_bytes_read")) for m in manifests]),
            "guest_linear_memory_mib": span([mb(m.get("guest_linear_memory_final")) for m in manifests]),
            "guest_elapsed_ms": span([m.get("elapsed_ms") for m in manifests]),
            "chunk_bytes": manifests[0].get("chunk_bytes") if manifests else None,
            "chunk_sha256": sorted({m.get("chunk_sha256") for m in manifests}),
            "diagnostics": sorted({x for m in manifests for x in (m.get("diagnostics") or [])}),
            "mounts_unchanged": all(s["mounts_unchanged"] for _, s in runs if s),
            "leaks": sorted({x for _, s in runs if s for j in s["jobs"] for x in (j.get("leaks") or [])}),
            "killed_by_memory_guard": any(s.get("killed_by_memory_guard") for _, s in runs if s),
        })
    return rows


def startup_rows(matrix):
    groups = defaultdict(list)
    for d in sorted(Path(matrix).glob("startup-*")):
        s = load(d / "summary.json")
        if s:
            groups[d.name.rsplit("-r", 1)[0]].append(s)
    return [{"run": k, "runs": len(v), "exit_codes": sorted({s["jobs"][0].get("exit_code") for s in v}),
             "terminal_s": span([s["jobs"][0].get("terminal_s") for s in v]),
             "node_peak_wset_minus_baseline_mib": span([mb(s["node_peak_wset_minus_baseline_rss"]) for s in v])}
            for k, v in groups.items()]


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--runs", required=True, type=Path)
    p.add_argument("--out", required=True, type=Path)
    a = p.parse_args()
    out = {
        "schema": "viii.pdf-chunker-alternatives.v1",
        "native": local_rows(a.runs / "native.jsonl"),
        "wasmtime": local_rows(a.runs / "wasmtime.jsonl"),
        "bacalhau": bacalhau_rows(a.runs / "matrix"),
        "bacalhau_startup_control": startup_rows(a.runs / "matrix"),
        "validation": {f.stem: load(f) for f in sorted((a.runs / "validate").glob("*.json"))},
        "adversarial": load(a.runs / "adversarial" / "adversarial.json"),
        "modules": {f.name: {"bytes": f.stat().st_size} for f in sorted(a.runs.glob("*.wasm"))},
    }
    a.out.write_text(json.dumps(out, indent=1), encoding="utf-8")
    print(f"wrote {a.out}")


if __name__ == "__main__":
    main()
