#!/usr/bin/env python3
"""Build compact result tables from raw run summaries (observed values only).

  python summarize.py --matrix DIR [--runs DIR] --out results/

Writes results/native.json, wasi.json, startup.json, concurrency.json, e2e.json
(compact, committed) and prints Markdown tables. Missing or failed runs are
reported as such, never estimated.
"""
import argparse
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


def native_rows(matrix):
    rows = []
    for d in sorted(matrix.glob("native-*")):
        s = load(d / "summary.json")
        if not s:
            rows.append({"run": d.name, "status": "no summary (aborted?)"})
            continue
        _, fixture, label = d.name.split("-", 2)
        for cand in ("pdfcpu", "pdq", "qpdf"):
            runs = [r for r in s["results"] if r["candidate"] == cand]
            ok = [r for r in runs if r.get("status") == "structurally_verified"]
            row = {"fixture": fixture, "pages": s["total_pages"], "bytes": s["original_bytes"], "range": label,
                   "first": s["first"], "last": s["last"], "candidate": cand, "runs": len(runs), "ok": len(ok),
                   "statuses": sorted({r.get("status") for r in runs})}
            if ok:
                ex = [r["exact"] for r in ok]
                row.update(
                    elapsed_median_s=statistics.median(r["elapsed_seconds"] for r in ok),
                    peak_wset_mb_median=mb(statistics.median(e.get("peak_working_set_bytes", e.get("peak_rss_bytes", 0)) for e in ex)),
                    peak_private_mb_max=mb(max(e.get("peak_private_bytes", 0) for e in ex)) if "peak_private_bytes" in ex[0] else None,
                    read_mb_median=mb(statistics.median(e["read_bytes"] for e in ex)) if all("read_bytes" in e for e in ex) else None,
                    cpu_s_median=round(statistics.median(e.get("cpu_user_s", 0) + e.get("cpu_kernel_s", 0) for e in ex), 3),
                    output_mb=mb(ok[0]["output_bytes"]),
                    deterministic=len({r["sha256"] for r in ok}) == 1,
                    content_equal=all(r.get("validation", {}).get("all_equal", True) for r in ok),
                    validated=any("validation" in r for r in ok))
            guard = [r for r in runs if r.get("killed_by_memory_guard")]
            if guard:
                row["memory_guard_aborts"] = len(guard)
                row["guard_lower_bound_peak_wset_mb"] = mb(max(r["exact"].get("peak_working_set_bytes", 0) for r in guard))
            row["input_unchanged"] = s["input_unchanged"]
            rows.append(row)
    return rows


def wasi_summary(s):
    j = s["jobs"][0] if s.get("jobs") else {}
    m = j.get("manifest") or {}
    return {"passed": s.get("passed"), "state": j.get("state"), "exit_code": j.get("exit_code"),
            "attempts": j.get("attempts"), "ready_s": j.get("ready_s"), "terminal_s": j.get("terminal_s"),
            "node_ready_s": s.get("node_ready_s"),
            "node_peak_wset_minus_baseline_mb": mb(s.get("node_peak_wset_minus_baseline_rss")),
            "node_peak_private_minus_baseline_mb": mb(s.get("node_peak_private_minus_baseline")),
            "node_baseline_rss_mb": mb(s.get("node_baseline_rss_bytes")),
            "node_read_mb": mb(s.get("node_read_bytes_during_jobs")),
            "node_cpu_s": s.get("node_cpu_s_during_jobs"),
            "chunk_mb": mb(m.get("chunk_bytes")), "chunk_sha256": m.get("chunk_sha256"),
            "go_runtime_sys_mb": mb(m.get("go_runtime_sys_bytes")),
            "node_data_mb": mb(s.get("node_data_bytes")),
            "killed_by_memory_guard": s.get("killed_by_memory_guard"),
            "min_host_available_mb": mb(s.get("sampled_min_host_available")),
            "mounts_unchanged": s.get("mounts_unchanged"), "leaks": j.get("leaks"),
            "module_sha256": (s.get("identities") or {}).get("module", {}).get("sha256")}


def wasi_rows(matrix):
    rows = []
    for d in sorted(matrix.glob("wasi-*")):
        s = load(d / "summary.json")
        parts = d.name.split("-")  # wasi-<cand>-<fixture>-<range>-r<rep>
        row = {"candidate": parts[1], "fixture": parts[2], "range": parts[3], "repeat": parts[4], "run": d.name}
        row.update(wasi_summary(s) if s else {"passed": False, "state": "no summary"})
        rows.append(row)
    return rows


def startup_rows(matrix):
    rows = []
    for d in sorted(matrix.glob("startup-*")):
        s = load(d / "summary.json")
        if s:
            j = s["jobs"][0]
            rows.append({"candidate": d.name.split("-")[1], "run": d.name, "exit_code": j.get("exit_code"),
                         "terminal_s": j.get("terminal_s"), "module_mb": mb(s["identities"]["module"]["bytes"]),
                         "node_peak_wset_minus_baseline_mb": mb(s.get("node_peak_wset_minus_baseline_rss")),
                         "node_peak_private_minus_baseline_mb": mb(s.get("node_peak_private_minus_baseline")),
                         "node_cpu_s": s.get("node_cpu_s_during_jobs")})
    return rows


def concurrency_rows(matrix):
    rows = []
    for d in sorted(matrix.glob("conc-*")):
        s = load(d / "summary.json")
        if not s:
            rows.append({"run": d.name, "passed": False, "state": "no summary"})
            continue
        jobs = s["jobs"]
        rows.append({"run": d.name, "jobs": len(jobs), "passed": s["passed"], "wall_s": s.get("wall_s"),
                     "ready_s": [j.get("ready_s") for j in jobs], "states": [j.get("state") for j in jobs],
                     "exit_codes": [j.get("exit_code") for j in jobs],
                     "node_peak_wset_minus_baseline_mb": mb(s.get("node_peak_wset_minus_baseline_rss")),
                     "node_peak_private_minus_baseline_mb": mb(s.get("node_peak_private_minus_baseline")),
                     "node_cpu_s": s.get("node_cpu_s_during_jobs"),
                     "min_host_available_mb": mb(s.get("sampled_min_host_available")),
                     "killed_by_memory_guard": s.get("killed_by_memory_guard")})
    return rows


def e2e_rows(runs):
    rows = []
    for d in sorted(runs.glob("e2e-*")):
        p = load(d / "provenance.json")
        if not p:
            continue
        rows.append({"run": d.name, "passed": p.get("passed"), "original_sha256": p["original"]["sha256_before"],
                     "original_unchanged": p["original"].get("unchanged"), "range": p["requested_physical_range"],
                     "derivative": p.get("derivative"), "citations": p.get("citations"),
                     "baseline_comparison": p.get("baseline_comparison"),
                     "steps": {k: {kk: v.get(kk) for kk in ("passed", "wall_s", "node_peak_wset_minus_baseline_rss",
                                                           "node_peak_private_minus_baseline", "killed_by_memory_guard",
                                                           "job_id", "execution_id", "executions")}
                               for k, v in p.get("steps", {}).items()},
                     "pages": p.get("pages")})
    return rows


def table(rows, cols):
    out = ["| " + " | ".join(cols) + " |", "|" + "---|" * len(cols)]
    for r in rows:
        out.append("| " + " | ".join("" if r.get(c) is None else str(r.get(c)) for c in cols) + " |")
    return "\n".join(out)


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--matrix", required=True, type=Path)
    p.add_argument("--runs", type=Path)
    p.add_argument("--out", required=True, type=Path)
    a = p.parse_args()
    a.out.mkdir(parents=True, exist_ok=True)
    data = {"native": native_rows(a.matrix), "wasi": wasi_rows(a.matrix), "startup": startup_rows(a.matrix),
            "concurrency": concurrency_rows(a.matrix), "e2e": e2e_rows(a.runs) if a.runs else []}
    for k, v in data.items():
        (a.out / f"{k}.json").write_text(json.dumps(v, indent=1), encoding="utf-8")
    print(table(data["native"], ["fixture", "range", "candidate", "ok", "elapsed_median_s", "peak_wset_mb_median",
                                 "peak_private_mb_max", "read_mb_median", "cpu_s_median", "output_mb",
                                 "deterministic", "content_equal", "memory_guard_aborts"]))
    print()
    print(table(data["wasi"], ["candidate", "fixture", "range", "repeat", "passed", "exit_code", "ready_s",
                               "node_peak_wset_minus_baseline_mb", "node_peak_private_minus_baseline_mb",
                               "node_read_mb", "node_cpu_s", "go_runtime_sys_mb", "chunk_mb", "killed_by_memory_guard"]))
    print()
    print(table(data["startup"], ["candidate", "run", "exit_code", "terminal_s", "module_mb",
                                  "node_peak_wset_minus_baseline_mb", "node_peak_private_minus_baseline_mb", "node_cpu_s"]))
    print()
    print(table(data["concurrency"], ["run", "jobs", "passed", "wall_s", "ready_s", "node_peak_wset_minus_baseline_mb",
                                      "node_peak_private_minus_baseline_mb", "node_cpu_s", "min_host_available_mb"]))


if __name__ == "__main__":
    main()
