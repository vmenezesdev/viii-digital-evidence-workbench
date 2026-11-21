#!/usr/bin/env python3
"""Build results/batch.json from batch_bench.py runs (BATCH-RESULTS.md).

  python batch_summarize.py --runs DIR [DIR ...] --fixtures fixtures.json \
      --historical historical.json --out results/batch.json

* groups runs by (mode, impl, fixture, chunks) and reports median and range
  of every metric over the successful repeats, plus failures/refusals;
* reproducibility: number of distinct chunk-set digests per group, and the
  cross-implementation equality of chunk boundaries;
* fits the memory/time model used by selection_model.py (least squares on
  PDF bytes and page count, restricted to non-negative coefficients);
* replays the selection policy on every measured scenario for a grid of node
  budgets and compares it with fixed choices.
No PDF content is read; only manifests and run records.
"""
import argparse
import json
from pathlib import Path
import statistics
import sys

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import selection_model as model  # noqa: E402

MiB = 1 << 20


def rows(dirs):
    out = []
    for d in dirs:
        f = Path(d) / "runs.jsonl"
        if f.exists():
            out += [json.loads(line) for line in f.read_text(encoding="utf-8").splitlines() if line.strip()]
    return out


def med(values):
    values = [v for v in values if v is not None]
    if not values:
        return None
    return {"median": statistics.median(values), "min": min(values), "max": max(values), "n": len(values)}


def metric_rows(r):
    """Flatten one run into the metrics reported per group."""
    w = r.get("worker") or {}
    m = {"chunk_count": r.get("chunk_count"), "chunk_bytes_total": r.get("chunk_bytes_total"),
         "chunk_bytes_max": r.get("chunk_bytes_max")}
    if r["mode"] == "native":
        ex = r.get("exact") or {}
        m |= {"total_s": r.get("elapsed_s"), "peak_working_set": ex.get("peak_working_set_bytes"),
              "peak_private": ex.get("peak_private_bytes"), "os_read_bytes": ex.get("read_bytes"),
              "os_write_bytes": ex.get("write_bytes"), "cpu_s": (ex.get("cpu_user_s") or 0) + (ex.get("cpu_kernel_s") or 0),
              "page_faults": ex.get("page_faults"), "host_verify_s": r.get("host_verify_s"),
              "first_chunk_s": (w.get("first_chunk_done_ms") or 0) / 1000 if w.get("first_chunk_done_ms") is not None
              else r.get("first_chunk_mtime_s"),
              "first_chunk_usable_s": (w.get("first_chunk_done_ms") or 0) / 1000 if w.get("first_chunk_done_ms") is not None
              else r.get("elapsed_s")}
    else:
        m |= {"ready_s": r.get("ready_s"), "copy_verify_s": r.get("copy_verify_s"), "total_s": r.get("all_available_s"),
              "first_chunk_s": (w.get("first_chunk_done_ms") or 0) / 1000 if w.get("first_chunk_done_ms") is not None else None,
              "first_chunk_usable_s": r.get("first_chunk_usable_s") or r.get("all_available_s"),
              "node_peak_wset": r.get("node_peak_wset_minus_baseline"),
              "node_peak_private": r.get("node_peak_private_minus_baseline"),
              "node_read_bytes": r.get("node_read_bytes"), "node_write_bytes": r.get("node_write_bytes"),
              "cpu_s": r.get("node_cpu_s"), "min_host_available": r.get("sampled_min_host_available")}
    io = w.get("io") or {}
    ph = w.get("phases_ms") or {}
    lin = w.get("linear_first_last") or [None, None]
    m |= {"guest_bytes_read": io.get("source_bytes_read"), "guest_read_calls": io.get("source_read_calls"),
          "guest_seeks": io.get("source_seeks"), "guest_linear_after_index": w.get("guest_linear_memory_after_index"),
          "guest_linear_first_chunk": lin[0], "guest_linear_final": w.get("guest_linear_memory_final"),
          "worker_elapsed_s": (w.get("elapsed_ms") or 0) / 1000 if w.get("elapsed_ms") is not None else None,
          "extract_ms_first_decile": w.get("extract_ms_first_decile_mean"),
          "extract_ms_last_decile": w.get("extract_ms_last_decile_mean")}
    for k in ("open", "page_tree", "first_push_inherited", "extract", "prune", "write", "verify"):
        m[f"phase_{k}_s"] = ph[k] / 1000 if ph.get(k) is not None else None
    if r.get("last") and m.get("total_s"):
        m["pages_per_s"] = r["last"] / m["total_s"]
    return m


def group(all_rows):
    groups = {}
    for r in all_rows:
        key = (r["mode"], r["impl"], r["fixture"], str(r["chunks_requested"]))
        groups.setdefault(key, []).append(r)
    out = []
    for (mode, impl, fixture, chunks), rs in sorted(groups.items()):
        ok = [r for r in rs if r.get("status") == "ok" and r.get("original_unchanged")]
        metrics = [metric_rows(r) for r in ok]
        keys = sorted({k for m in metrics for k in m})
        out.append({"mode": mode, "impl": impl, "fixture": fixture, "chunks": chunks, "last_page": rs[0]["last"],
                    "env": rs[0].get("env") or {}, "runs": len(rs), "ok": len(ok),
                    "failed": [{"repeat": r["repeat"], "status": r.get("status"),
                                "memory_guard": r.get("killed_by_memory_guard"),
                                "host_available_before_mib": (r.get("admission") or {}).get("host_available_before_bytes", 0) >> 20,
                                "reason": r.get("verify_reason") or r.get("stderr", "")[:200]}
                               for r in rs if r not in ok],
                    "set_sha256": sorted({r.get("set_sha256") for r in ok}),
                    "reproducible": len({r.get("set_sha256") for r in ok}) == 1 if ok else None,
                    "original_unchanged": all(r.get("original_unchanged") for r in rs),
                    "leaks": sorted({x for r in rs for x in (r.get("leaks") or [])}),
                    "module_sha256": sorted({r["module_sha256"] for r in ok if r.get("module_sha256")}),
                    "extract_route": sorted({(r.get("worker") or {}).get("extract_route") for r in ok} - {None}),
                    "metrics": {k: med([m.get(k) for m in metrics]) for k in keys}})
    return out


def lstsq_nonneg(samples):
    """samples: [(bytes, pages, y)]; fit y = a + b*bytes + c*pages with b, c >= 0."""
    def solve(cols):
        # normal equations, tiny dense Gauss-Jordan
        X = [[1.0] + [s[i] for i in cols] for s in samples]
        y = [s[2] for s in samples]
        n = len(X[0])
        A = [[sum(X[k][i] * X[k][j] for k in range(len(X))) for j in range(n)] + [sum(X[k][i] * y[k] for k in range(len(X)))]
             for i in range(n)]
        for i in range(n):
            piv = max(range(i, n), key=lambda r: abs(A[r][i]))
            A[i], A[piv] = A[piv], A[i]
            if abs(A[i][i]) < 1e-12:
                return None
            for r in range(n):
                if r != i:
                    f = A[r][i] / A[i][i]
                    A[r] = [a - f * b for a, b in zip(A[r], A[i])]
        coef = [A[i][n] / A[i][i] for i in range(n)]
        full = [coef[0], 0.0, 0.0]
        for k, i in enumerate(cols):
            full[1 + i] = coef[1 + k]
        return full
    for cols in ([0, 1], [0], [1]):
        if len(samples) <= len(cols):
            continue
        c = solve(cols)
        if c and all(v >= 0 for v in c[1:]):
            return c
    return [statistics.mean(s[2] for s in samples), 0.0, 0.0]


def fit(groups, fixtures, historical):
    """Fit node memory and time-to-all-available for the full-document WASI runs."""
    coeffs = {}
    samples = {}
    for g in groups:
        if g["mode"] != "wasi" or g["chunks"] != "all" or not g["ok"]:
            continue
        fx = fixtures[g["fixture"]]
        samples.setdefault(g["impl"], []).append(
            (fx["bytes"], fx["pages"], g["metrics"]["node_peak_wset"]["median"], g["metrics"]["total_s"]["median"],
             (g["metrics"].get("guest_linear_final") or {}).get("median")))
    for h in historical:  # e.g. pdq-wasi s30000 batch from RESULTS.md, clearly labelled
        fx = fixtures[h["fixture"]]
        samples.setdefault(h["impl"], []).append((fx["bytes"], fx["pages"], h["node_peak_wset"], h["total_s"], None))
    for impl, ss in samples.items():
        strategy = "pdq-wasi" if impl.startswith("pdq") else f"{impl}-wasi"
        # A pre-sized module (--initial-memory) reports its fixed size, not its need:
        # only samples below the 128 MiB pre-size inform the linear-memory model.
        lin = [(b, p, l) for b, p, _, _, l in ss if l and l < 128 * MiB]
        coeffs[strategy] = {"mem": lstsq_nonneg([(b, p, m) for b, p, m, _, _ in ss]),
                            "time": lstsq_nonneg([(b, p, t) for b, p, _, t, _ in ss]),
                            "linear": ([0, 1.0, 0.0] if impl.startswith("pdq")  # whole file in linear memory
                                       else lstsq_nonneg(lin) if len(lin) >= 3
                                       else [0, 0.0, max(l / p for _, p, l in lin)] if lin else [0, 0.0, 0.0]),
                            "samples": len(ss)}
    return coeffs


def replay(groups, fixtures, coeffs, historical):
    """Policy vs fixed choices on each measured full-document scenario and budget."""
    measured = {}
    for g in groups:
        if g["mode"] == "wasi" and g["chunks"] == "all" and g["ok"]:
            measured[(g["impl"], g["fixture"])] = (g["metrics"]["node_peak_wset"]["median"], g["metrics"]["total_s"]["median"])
    for h in historical:
        measured.setdefault((h["impl"], h["fixture"]), (h["node_peak_wset"], h["total_s"]))
    impls = sorted({i for i, _ in measured})
    flpdf_impl = next((i for i in impls if i.startswith("flpdf-lean")), next((i for i in impls if i.startswith("flpdf")), None))
    cases = []
    for fixture, fx in fixtures.items():
        if ("pdq", fixture) not in measured or (flpdf_impl, fixture) not in measured:
            continue
        for avail_gib in (1.5, 2, 3, 4, 8):
            for reserved_gib in (0, 1):
                budget = avail_gib * 1024 * MiB
                d = model.choose(fx["bytes"], fx["pages"], budget, reserved_gib * 1024 * MiB, margin=512 * MiB,
                                 coeffs=coeffs | {"flpdf-wasi": coeffs.get(f"{flpdf_impl}-wasi", model.DEFAULTS["flpdf-wasi"])})
                real = {"pdq-wasi": measured[("pdq", fixture)], "flpdf-wasi": measured[(flpdf_impl, fixture)]}
                cap = budget - reserved_gib * 1024 * MiB

                def outcome(s):
                    if s == "reject":
                        return {"ok": False, "time_s": None, "fits": None}
                    mem, t = real[s]
                    return {"ok": mem <= cap, "time_s": t, "fits": mem <= cap}
                fits = {s: outcome(s) for s in real}
                feasible = [s for s in real if fits[s]["ok"]]
                oracle = min(feasible, key=lambda s: real[s][1]) if feasible else "reject"
                cases.append({"fixture": fixture, "available_gib": avail_gib, "reserved_gib": reserved_gib,
                              "policy": d["choice"], "policy_outcome": outcome(d["choice"]),
                              "fixed_pdq": fits["pdq-wasi"], "fixed_flpdf": fits["flpdf-wasi"], "oracle": oracle,
                              "pdq_speed_advantage_s": round(real["flpdf-wasi"][1] - real["pdq-wasi"][1], 2),
                              "estimates": {s: {k: d[s][k] for k in ("memory", "time_s", "safe")} for s in real}})
    def score(key):
        done = [c for c in cases if c[key]["ok"]]
        rejected = [c for c in cases if c[key]["time_s"] is None]
        exceeded = [c for c in cases if c[key]["time_s"] is not None and not c[key]["ok"]]
        extra = [c[key]["time_s"] - min(t for s, t in (("p", c["fixed_pdq"]["time_s"]), ("f", c["fixed_flpdf"]["time_s"]))
                                         if c[{"p": "fixed_pdq", "f": "fixed_flpdf"}[s]]["ok"]) for c in done]
        return {"completed": len(done), "rejected_by_policy": len(rejected),
                "exceeded_memory_budget": len(exceeded), "time_s_sum_completed": round(sum(c[key]["time_s"] for c in done), 1),
                "time_s_over_fastest_feasible": round(sum(extra), 1)}
    adv = []
    for fixture, fx in fixtures.items():
        if ("pdq", fixture) in measured and (flpdf_impl, fixture) in measured:
            (pm, pt), (fm, ft) = measured[("pdq", fixture)], measured[(flpdf_impl, fixture)]
            adv.append({"fixture": fixture, "pdq_time_s": pt, "flpdf_time_s": ft, "pdq_speedup_x": round(ft / pt, 2),
                        "pdq_saves_s": round(ft - pt, 1), "pdq_node_mib": round(pm / MiB), "flpdf_node_mib": round(fm / MiB),
                        "flpdf_memory_saving_x": round(pm / fm, 2)})
    return {"flpdf_variant": flpdf_impl, "scenario_advantages": adv,
            "note": "in-sample: the model was fitted on the same scenarios it is replayed on", "cases": cases,
            "score": {"policy": score("policy_outcome"), "fixed_pdq": score("fixed_pdq"), "fixed_flpdf": score("fixed_flpdf")},
            "policy_matches_oracle": sum(c["policy"] == c["oracle"] for c in cases), "cases_total": len(cases)}


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--runs", nargs="+", required=True)
    p.add_argument("--fixtures", required=True, type=Path, help="JSON {name: {bytes, pages, sha256, kind}}")
    p.add_argument("--historical", type=Path, help="JSON list of labelled historical baselines")
    p.add_argument("--extra", type=Path, help="JSON merged into the output (identities, validation, e2e)")
    p.add_argument("--out", required=True, type=Path)
    a = p.parse_args()
    fixtures = json.loads(a.fixtures.read_text(encoding="utf-8"))
    historical = json.loads(a.historical.read_text(encoding="utf-8")) if a.historical else []
    groups = group(rows(a.runs))
    coeffs = fit(groups, fixtures, historical)
    out = {"schema": "viii.pdf-chunk-batch-results.v1", "fixtures": fixtures, "historical_baselines": historical,
           "groups": groups,
           "model": {"form": "y = a + b*pdf_bytes + c*pages (b, c >= 0), fitted on full-document WASI runs",
                     "coefficients": coeffs},
           "policy_replay": replay(groups, fixtures, coeffs, historical)}
    if a.extra:
        out |= json.loads(a.extra.read_text(encoding="utf-8"))
    a.out.write_text(json.dumps(out, indent=1) + "\n", encoding="utf-8")
    print(f"wrote {a.out}: {len(groups)} groups")


if __name__ == "__main__":
    raise SystemExit(main())
