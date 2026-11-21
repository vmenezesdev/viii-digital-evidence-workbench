#!/usr/bin/env python3
"""Guarded batch-chunking comparison (BATCH-RESULTS.md).

  python batch_bench.py native --impl pdq=pdq-native.exe flpdf=flpdf-native.exe \
      --fixture s1000=s1000.pdf --chunks 10 50 all --repeat 3 --out DIR
  python batch_bench.py wasi --impl pdq=pdq.wasm flpdf=flpdf.wasm \
      --fixture s1000=s1000.pdf --chunks 10 50 all --repeat 2 --memory 1gb --out DIR

Each run splits physical pages [1, min(N, k*size)] of a fixture into chunks of
`--size` pages (k = 10, 50 or all), in ONE invocation of the implementation:

* native: the batch contract of each worker built natively. pdq: pdq-wasi's
  positional batch contract (`orig 1 L out ack sha size`; the ack is created
  beforehand so the worker exits at once; native pdq uses its own mmap
  loader, the WASI patch is cfg(wasi) only). flpdf: `--chunk-size --out-dir`.
  Exact OS peaks (working set, private), CPU, OS I/O from the process handle
  (bench.run_one), host memory guard and timeout.
* wasi: one Bacalhau job on a fresh pinned node (bacalhau_wasi.run) with a job
  memory limit, the ready/ack protocol, a chunk-set verification of every
  chunk before the ack and, for workers that write sidecars, the first chunk
  copied and verified while the batch is still running (`progressive`).

Admission: before every run the host's available memory must cover the
estimated peak (selection_model.estimate, conservative pre-fit defaults or the
fitted model) plus `--margin-mib`; otherwise the run is recorded as `refused`
and is not started. The original's sha256 is checked before and after.
Chunk PDFs of repeats > 1 are deleted after their hashes were recorded (disk).
One JSON object per run is appended to OUT/runs.jsonl.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sys
import time

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
import bacalhau_wasi as runner  # noqa: E402
from bench import run_one  # noqa: E402
import selection_model as model  # noqa: E402

try:
    import psutil
except ImportError:  # pragma: no cover
    psutil = None

LEAK_MARKERS = ["JABUTICABA", "Documento", "prova documental", "ARARA"]


def pairs(values):
    out = {}
    for v in values:
        k, _, path = v.partition("=")
        if not path:
            raise SystemExit(f"expected name=path, got {v}")
        out[k] = Path(path).resolve(strict=True)
    return out


def fixture_pages(pdf):
    meta = Path(pdf).with_suffix(".json")
    if not meta.exists():
        raise SystemExit(f"{meta} (fixture manifest) is required to know the page count")
    return json.loads(meta.read_text(encoding="utf-8"))["pages"]


def last_page(pages, chunks, size):
    return pages if chunks == "all" else min(pages, int(chunks) * size)


def set_digest(chunks):
    """Identity of a whole chunk set: sha256 over 'first-last:sha' lines in order."""
    lines = "".join(f"{c['physical_first']}-{c['physical_last']}:{c['chunk_sha256']}\n" for c in chunks)
    return hashlib.sha256(lines.encode()).hexdigest()


def available():
    return psutil.virtual_memory().available if psutil else None


def base(impl):
    """Model strategy family of an implementation name (flpdf-lean -> flpdf)."""
    return impl.split("-")[0]


def impl_env(a, impl):
    """--env flpdf-lean=FLPDF_EXTRACT=lean,CHUNKER_READ_BUF=2048"""
    for spec in a.env:
        name, _, kvs = spec.partition("=")
        if name == impl:
            return dict(kv.split("=", 1) for kv in kvs.split(",") if kv)
    return {}


def admit(strategy, pdf, pages, margin):
    est = model.estimate(strategy, pdf.stat().st_size, pages, model.load())
    avail = available()
    ok = avail is None or est["memory"] + margin <= avail
    return ok, {"estimated_peak_bytes": est["memory"], "host_available_before_bytes": avail, "margin_bytes": margin}


def chunk_mtimes(out_dir, t_start):
    """First/last chunk completion (file mtime) relative to process start."""
    times = sorted(p.stat().st_mtime for p in Path(out_dir).glob("chunk-*.pdf"))
    if not times:
        return None, None
    return round(times[0] - t_start, 3), round(times[-1] - t_start, 3)


def drop_pdfs(out_dir):
    for p in Path(out_dir).glob("chunk-*.pdf"):
        p.unlink()


def native_run(a, impl, exe, fixture, pdf, pages, chunks, rep, sha):
    last = last_page(pages, chunks, a.size)
    tag = f"native-{impl}-{fixture}-k{chunks}-r{rep}"
    out_dir = a.out / tag
    ok, adm = admit(f"{base(impl)}-native", pdf, pages, a.margin_mib * model.MiB)
    row = {"mode": "native", "impl": impl, "fixture": fixture, "chunks_requested": chunks, "first": 1, "last": last,
           "repeat": rep, "tag": tag, "admission": adm, "at": time.strftime("%Y-%m-%dT%H:%M:%S")}
    if not ok:
        return row | {"status": "refused"}
    out_dir.mkdir(parents=True)
    if base(impl) == "pdq":
        ack = a.out / f"{tag}.ack"
        ack.write_text("accepted", encoding="utf-8")
        cmd = [str(exe), str(pdf), "1", str(last), str(out_dir), str(ack), sha, str(a.size)]
    else:
        cmd = [str(exe), str(pdf), "--range", f"1-{last}", "--chunk-size", str(a.size), "--out-dir", str(out_dir)]
    env = impl_env(a, impl)
    saved = {k: os.environ.get(k) for k in env}
    os.environ.update(env)  # run_one's Popen inherits the environment
    try:
        t_start = time.time()
        r = run_one(impl, cmd, out_dir / "manifest.json", a.timeout, a.out, tag=tag,
                    min_available=a.min_available_mib << 20)
    finally:
        for k, v in saved.items():
            if v is None:
                os.environ.pop(k, None)
            else:
                os.environ[k] = v
    row["env"] = env
    first_mtime, last_mtime = chunk_mtimes(out_dir, t_start)
    t_v = time.perf_counter()
    verified, reason, _ = runner.verify_chunkset_dir(out_dir, 1, last)
    host_verify_s = round(time.perf_counter() - t_v, 3)
    manifest = json.loads((out_dir / "manifest.json").read_text(encoding="utf-8")) if verified else {}
    row.update(status="ok" if r["returncode"] == 0 and verified else "failed", returncode=r["returncode"],
               elapsed_s=r["elapsed_seconds"], exact=r["exact"], killed_by_memory_guard=r["killed_by_memory_guard"],
               timed_out=r["timed_out"], min_host_available_bytes=r["min_host_available_bytes"],
               stderr=Path(r["stderr_log"]).read_text(encoding="utf-8", errors="replace")[:1000],
               verified=verified, verify_reason=reason, host_verify_s=host_verify_s,
               first_chunk_mtime_s=first_mtime, last_chunk_mtime_s=last_mtime)
    row["leaks"] = runner.find_leaks(row["stderr"] + Path(r["stdout_log"]).read_text(errors="replace"), LEAK_MARKERS)
    row |= summarize_manifest(manifest)
    if rep > 1 and not a.keep:
        drop_pdfs(out_dir)
    return row


def summarize_manifest(m):
    if not m:
        return {}
    chunks = m.get("chunks", [])
    out = {"chunk_count": len(chunks), "chunk_bytes_total": sum(c["chunk_bytes"] for c in chunks),
           "chunk_bytes_max": max((c["chunk_bytes"] for c in chunks), default=0),
           "set_sha256": set_digest(chunks), "first_chunk_sha256": chunks[0]["chunk_sha256"] if chunks else None,
           "worker": {k: m.get(k) for k in ("tool", "tool_version", "source_page_count", "phases_ms", "io", "extract_route",
                                            "guest_linear_memory_after_open", "guest_linear_memory_after_index",
                                            "guest_linear_memory_final", "elapsed_ms", "split_ms", "diagnostics")
                      if k in m}}
    metrics = [c.get("metrics") for c in chunks if c.get("metrics")]
    if metrics:
        out["worker"]["first_chunk_done_ms"] = metrics[0]["done_ms"]
        # Growth across the batch: linear memory and per-chunk extraction time.
        lin = [x["guest_linear_memory"] for x in metrics if x.get("guest_linear_memory") is not None]
        ext = [x["extract_ms"] for x in metrics]
        q = max(1, len(ext) // 10)
        out["worker"]["linear_first_last"] = [lin[0], lin[-1]] if lin else None
        out["worker"]["extract_ms_first_decile_mean"] = round(sum(ext[:q]) / q, 2)
        out["worker"]["extract_ms_last_decile_mean"] = round(sum(ext[-q:]) / q, 2)
        out["worker"]["source_bytes_read_per_chunk_mean"] = round(sum(x["source_bytes_read"] for x in metrics) / len(metrics))
    return out


def wasi_run(a, impl, module, fixture, pdf, pages, chunks, rep, sha):
    last = last_page(pages, chunks, a.size)
    tag = f"wasi-{impl}-{fixture}-k{chunks}-r{rep}"
    res = (a.out / tag).resolve()
    ok, adm = admit(f"{base(impl)}-wasi", pdf, pages, a.margin_mib * model.MiB)
    row = {"mode": "wasi", "impl": impl, "fixture": fixture, "chunks_requested": chunks, "first": 1, "last": last,
           "repeat": rep, "tag": tag, "admission": adm, "memory_limit": a.memory, "env": impl_env(a, impl),
           "at": time.strftime("%Y-%m-%dT%H:%M:%S")}
    if not ok:
        return row | {"status": "refused"}
    plan = {"module": str(module), "mounts": {"/original.pdf": str(pdf)}, "ready_protocol": True,
            "verify": "chunkset", "progressive": True, "memory": a.memory, "timeout_s": a.timeout,
            "min_available_mb": a.min_available_mib, "leak_markers": LEAK_MARKERS,
            "jobs": [{"id": "batch", "args": ["/original.pdf", "1", str(last), "/out", "/control/batch.ack", sha,
                                              str(a.size)], "env": impl_env(a, impl)}]}
    s = runner.run(plan, res, a.bacalhau)
    j = s["jobs"][0]
    art = res / "artifacts" / "batch"
    manifest = {}
    if (art / "manifest.json").exists():
        manifest = json.loads((art / "manifest.json").read_text(encoding="utf-8"))
    fc = j.get("first_chunk") or {}
    row.update(status="ok" if s["passed"] and j.get("verified") else "failed", passed=s["passed"],
               exit_code=j.get("exit_code"), attempts=j.get("attempts"), verified=j.get("verified"),
               verify_reason=j.get("verify_reason"), leaks=j.get("leaks"), ready_s=j.get("ready_s"),
               copy_verify_s=j.get("copy_verify_s"), terminal_s=j.get("terminal_s"),
               all_available_s=round((j.get("ready_s") or 0) + (j.get("copy_verify_s") or 0), 3),
               first_chunk_usable_s=fc.get("usable_s"), first_chunk_verified=fc.get("verified"),
               killed_by_memory_guard=s.get("killed_by_memory_guard"), mounts_unchanged=s.get("mounts_unchanged"),
               node_peak_wset_minus_baseline=s.get("node_peak_wset_minus_baseline_rss"),
               node_peak_private_minus_baseline=s.get("node_peak_private_minus_baseline"),
               node_baseline_rss=s.get("node_baseline_rss_bytes"), node_read_bytes=s.get("node_read_bytes_during_jobs"),
               node_write_bytes=s.get("node_write_bytes_during_jobs"), node_cpu_s=s.get("node_cpu_s_during_jobs"),
               sampled_min_host_available=s.get("sampled_min_host_available"), artifact_bytes=s.get("artifact_bytes"),
               module_sha256=s["identities"]["module"]["sha256"], results=str(res))
    row |= summarize_manifest(manifest)
    if rep > 1 and not a.keep:
        drop_pdfs(art)
        shutil.rmtree(res / "artifacts" / "batch-first-chunk", ignore_errors=True)
    return row


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("mode", choices=["native", "wasi"])
    p.add_argument("--impl", nargs="+", required=True, help="name=path (pdq=..., flpdf=...)")
    p.add_argument("--fixture", nargs="+", required=True, help="name=path (a NAME.json manifest must sit beside it)")
    p.add_argument("--chunks", nargs="+", default=["10", "50", "all"])
    p.add_argument("--size", type=int, default=40)
    p.add_argument("--repeat", type=int, default=2)
    p.add_argument("--out", required=True, type=Path)
    p.add_argument("--memory", default="1gb", help="Bacalhau job memory limit (wasi)")
    p.add_argument("--timeout", type=int, default=1800)
    p.add_argument("--min-available-mib", type=int, default=1024, help="host guard during a run")
    p.add_argument("--margin-mib", type=int, default=1024, help="admission margin above the estimate")
    p.add_argument("--keep", action="store_true", help="keep chunk PDFs of every repeat")
    p.add_argument("--env", nargs="*", default=[], help="IMPL=K=V[,K=V] environment for one implementation")
    p.add_argument("--bacalhau", type=Path, default=Path(r"C:\Bacalhau\bin\bacalhau.exe"))
    a = p.parse_args(argv)
    a.out.mkdir(parents=True, exist_ok=True)
    impls, fixtures = pairs(a.impl), pairs(a.fixture)
    others = runner.other_bacalhau_processes(a.bacalhau)
    if others:
        raise SystemExit(f"refusing: other Bacalhau processes are running {others}")
    rc = 0
    for fixture, pdf in fixtures.items():
        pages, sha = fixture_pages(pdf), runner.sha256(pdf)
        for chunks in a.chunks:
            if chunks != "all" and int(chunks) * a.size >= pages:
                continue  # the full run covers it
            for rep in range(1, a.repeat + 1):
                for impl, path in impls.items():  # interleave implementations
                    print("RUN", a.mode, impl, fixture, chunks, rep, flush=True)
                    fn = native_run if a.mode == "native" else wasi_run
                    row = fn(a, impl, path, fixture, pdf, pages, chunks, rep, sha)
                    row["original_sha256"] = sha
                    row["original_unchanged"] = runner.sha256(pdf) == sha
                    with (a.out / "runs.jsonl").open("a", encoding="utf-8") as log:
                        log.write(json.dumps(row) + "\n")
                    brief = {k: row.get(k) for k in ("status", "elapsed_s", "ready_s", "all_available_s",
                                                      "first_chunk_usable_s", "chunk_count", "set_sha256")}
                    mem = row.get("exact", {}).get("peak_private_bytes") or row.get("node_peak_wset_minus_baseline")
                    print("   ", json.dumps(brief | {"peak_mib": round((mem or 0) / 2**20, 1)}), flush=True)
                    if row["status"] != "ok" or not row["original_unchanged"]:
                        rc = 1
    return rc


if __name__ == "__main__":
    raise SystemExit(main())
