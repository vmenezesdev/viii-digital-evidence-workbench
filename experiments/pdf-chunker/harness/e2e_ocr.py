#!/usr/bin/env python3
"""End-to-end provenance gate: WASI chunker -> WASI OCR on the derivative.

All steps are real jobs on isolated, pinned Bacalhau nodes (bacalhau_wasi.py):
  1. chunk:     <chunker.wasm> extracts physical pages [first,last] of the original;
  2. ocr-chunk: workers/pdf (MVP pdf-worker.wasm, mode ocr) processes the
                derivative's local pages 1..n;
  3. baseline:  optionally the same worker processes [first,last] of the
                original directly (current MVP behaviour) for comparison.
Local results are mapped to physical pages only through the chunk manifest, and
a search for --term must cite the physical page of the ORIGINAL evidence.
Writes provenance.json; never prints page text (only counts and booleans).
"""
import argparse
import datetime as dt
import json
from pathlib import Path
import sys

sys.path.insert(0, str(Path(__file__).resolve().parent))
import bacalhau_wasi as runner  # noqa: E402

SCHEMA = "viii.pdf-chunk-provenance.v1"


def map_results(manifest, local_pages):
    """Map {local: page_json} through the manifest; returns [{physical, local, ...}]."""
    by_local = {p["local"]: p["physical"] for p in manifest["pages"]}
    if sorted(by_local) != list(range(1, len(by_local) + 1)):
        raise ValueError("manifest local pages are not 1..n")
    mapped = []
    for local, page in sorted(local_pages.items()):
        if local not in by_local:
            raise ValueError(f"result for local page {local} outside the chunk")
        if page.get("page") != local:
            raise ValueError(f"page file for local {local} reports {page.get('page')}")
        mapped.append({"physical": by_local[local], "local": local, "status": page.get("status"),
                       "source": page.get("source"), "tool": page.get("tool"), "version": page.get("version"),
                       "text": page.get("text") or ""})
    return mapped


def search(mapped, term):
    """Citations for term: physical page of the original + derivative locator."""
    return [{"physical_page": m["physical"], "chunk_local_page": m["local"], "source": m["source"]}
            for m in mapped if term.lower() in m["text"].lower()]


def load_pages(directory, first, last):
    pages = {}
    for n in range(first, last + 1):
        pages[n] = json.loads((Path(directory) / f"page-{n:06d}.json").read_text(encoding="utf-8"))
    return pages


def run_step(results, name, plan, bacalhau):
    plan_path = results / f"plan-{name}.json"
    plan_path.write_text(json.dumps(plan, indent=2), encoding="utf-8")
    return runner.run(plan, results / name, bacalhau)


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--original", required=True, type=Path)
    p.add_argument("--first", required=True, type=int)
    p.add_argument("--last", required=True, type=int)
    p.add_argument("--chunker", required=True, type=Path, help="chunker .wasm (pdfcpu or pdq worker)")
    p.add_argument("--worker", required=True, type=Path, help="workers/pdf pdf-worker.wasm")
    p.add_argument("--model-dir", required=True, type=Path)
    p.add_argument("--term", default="JABUTICABA")
    p.add_argument("--expect-page", type=int, help="physical page where --term must be found")
    p.add_argument("--baseline", action="store_true", help="also OCR the original range directly")
    p.add_argument("--results", required=True, type=Path)
    p.add_argument("--memory", default="3gb")
    p.add_argument("--min-available-mb", type=int, default=600)
    p.add_argument("--bacalhau", type=Path, default=Path(r"C:\Bacalhau\bin\bacalhau.exe"))
    a = p.parse_args(argv)
    if a.results.exists():
        p.error("results directory already exists")
    a.results.mkdir(parents=True)
    results = a.results.resolve()
    bacalhau = a.bacalhau.resolve(strict=True)
    original = a.original.resolve(strict=True)
    original_sha = runner.sha256(original)
    markers = [a.term, "Documento", "prova documental", "ARARA"]
    record = {"schema": SCHEMA, "created_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
              "original": {"path": str(original), "sha256_before": original_sha, "bytes": original.stat().st_size},
              "requested_physical_range": [a.first, a.last], "term": a.term, "steps": {}}
    common = {"memory": a.memory, "timeout_s": 3600, "leak_markers": markers, "min_available_mb": a.min_available_mb,
              "ready_protocol": True}

    chunk = run_step(results, "chunk", dict(common, module=str(a.chunker), mounts={"/original.pdf": str(original)},
                                            jobs=[{"id": "chunk", "args": ["/original.pdf", str(a.first), str(a.last),
                                                                           "/out", "/control/chunk.ack", original_sha]}]),
                     bacalhau)
    job = chunk["jobs"][0]
    record["steps"]["chunk"] = {k: chunk.get(k) for k in ("passed", "wall_s", "node_peak_wset_minus_baseline_rss",
                                                           "node_peak_private_minus_baseline", "identities",
                                                           "killed_by_memory_guard")}
    record["steps"]["chunk"].update(job_id=job.get("job_id"), execution_id=job.get("execution_id"),
                                    executions=job.get("executions"), ready_s=job.get("ready_s"))
    if not chunk["passed"]:
        record["outcome"] = "chunk step failed"
        (results / "provenance.json").write_text(json.dumps(record, indent=2), encoding="utf-8")
        print(json.dumps({"passed": False, "reason": record["outcome"]}))
        return 1
    manifest = job["manifest"]
    chunk_pdf = results / "chunk" / "artifacts" / "chunk" / "chunk.pdf"
    n = manifest["chunk_page_count"]
    record["derivative"] = {"role": "operational derivative, not evidence", "path": str(chunk_pdf),
                            "sha256": manifest["chunk_sha256"], "bytes": manifest["chunk_bytes"], "pages": n,
                            "physical_first": manifest["physical_first"], "physical_last": manifest["physical_last"],
                            "source_sha256_declared": manifest.get("source_sha256_declared"),
                            "chunker_tool": manifest["tool"], "chunker_tool_version": manifest["tool_version"],
                            "chunker_module_sha256": chunk["identities"]["module"]["sha256"]}

    worker_mounts = {"/original.pdf": str(chunk_pdf), "/model": str(a.model_dir.resolve(strict=True))}
    ocr = run_step(results, "ocr-chunk", dict(common, module=str(a.worker), mounts=worker_mounts, verify="pages",
                                              jobs=[{"id": "ocr", "args": ["/original.pdf", "1", str(n), "/out",
                                                                           "/control/ocr.ack", "ocr", "/model"]}]),
                   bacalhau)
    ojob = ocr["jobs"][0]
    record["steps"]["ocr_chunk"] = {k: ocr.get(k) for k in ("passed", "wall_s", "node_peak_wset_minus_baseline_rss",
                                                             "node_peak_private_minus_baseline", "identities",
                                                             "killed_by_memory_guard")}
    record["steps"]["ocr_chunk"].update(job_id=ojob.get("job_id"), execution_id=ojob.get("execution_id"),
                                        executions=ojob.get("executions"), ready_s=ojob.get("ready_s"))
    mapped = []
    if ocr["passed"]:
        mapped = map_results(manifest, load_pages(results / "ocr-chunk" / "artifacts" / "ocr", 1, n))
    record["pages"] = [{k: v for k, v in m.items() if k != "text"} | {"chars": len(m["text"].strip())} for m in mapped]
    record["citations"] = search(mapped, a.term)

    if a.baseline:
        base = run_step(results, "ocr-original", dict(common, module=str(a.worker), verify="pages",
                                                      mounts={"/original.pdf": str(original),
                                                              "/model": str(a.model_dir.resolve())},
                                                      jobs=[{"id": "ocr", "args": ["/original.pdf", str(a.first),
                                                                                   str(a.last), "/out",
                                                                                   "/control/ocr.ack", "ocr", "/model"]}]),
                        bacalhau)
        record["steps"]["ocr_original_baseline"] = {k: base.get(k) for k in (
            "passed", "wall_s", "node_peak_wset_minus_baseline_rss", "node_peak_private_minus_baseline",
            "killed_by_memory_guard")}
        if base["passed"] and mapped:
            direct = load_pages(results / "ocr-original" / "artifacts" / "ocr", a.first, a.last)
            same = [m["physical"] for m in mapped if (direct[m["physical"]].get("text") or "") == m["text"]
                    and direct[m["physical"]].get("source") == m["source"]]
            record["baseline_comparison"] = {"pages": len(mapped), "identical_text_and_source": len(same),
                                             "differs": sorted(set(m["physical"] for m in mapped) - set(same)),
                                             "baseline_citations": [{"physical_page": n_, "source": d.get("source")}
                                                                    for n_, d in sorted(direct.items())
                                                                    if a.term.lower() in (d.get("text") or "").lower()]}
    after = runner.sha256(original)
    record["original"]["sha256_after"] = after
    record["original"]["unchanged"] = after == original_sha
    expect_ok = a.expect_page is None or [c["physical_page"] for c in record["citations"]] == [a.expect_page]
    record["passed"] = bool(chunk["passed"] and ocr["passed"] and record["original"]["unchanged"] and expect_ok
                            and len(mapped) == n)
    (results / "provenance.json").write_text(json.dumps(record, indent=2), encoding="utf-8")
    print(json.dumps({"passed": record["passed"], "citations": record["citations"],
                      "derivative_sha256": record["derivative"]["sha256"],
                      "baseline_comparison": record.get("baseline_comparison"),
                      "steps": {k: {kk: v.get(kk) for kk in ("passed", "wall_s", "node_peak_wset_minus_baseline_rss")}
                                for k, v in record["steps"].items()}}, indent=2))
    return 0 if record["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
