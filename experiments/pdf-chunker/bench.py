#!/usr/bin/env python3
"""Run an isolated, reproducible PDF page-range extraction comparison.

Requires qpdf for output validation and psutil for RAM measurement.
Never modifies the input; output directory must not exist.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import subprocess
import sys
import time

try:
    import psutil
except ImportError:
    psutil = None


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            digest.update(block)
    return digest.hexdigest()


def capture(command, timeout=120):
    return subprocess.run(command, text=True, capture_output=True, timeout=timeout)


def page_count(qpdf, path):
    done = capture([qpdf, "--show-npages", str(path)])
    if done.returncode:
        raise RuntimeError(f"qpdf page count: {done.stderr.strip()}")
    return int(done.stdout.strip())


def peak_tree_rss(proc):
    if psutil is None:
        return None
    try:
        root = psutil.Process(proc.pid)
        processes = [root] + root.children(recursive=True)
        return sum(p.memory_info().rss for p in processes if p.is_running())
    except (psutil.NoSuchProcess, psutil.AccessDenied, psutil.ZombieProcess):
        return None


def run_one(name, cmd, output, timeout, logdir):
    stdout = logdir / f"{name}.stdout.log"
    stderr = logdir / f"{name}.stderr.log"
    started = time.perf_counter()
    peak = 0
    samples = 0
    timed_out = False
    with stdout.open("wb") as so, stderr.open("wb") as se:
        proc = subprocess.Popen(cmd, stdout=so, stderr=se)
        while proc.poll() is None:
            rss = peak_tree_rss(proc)
            if rss is not None:
                peak = max(peak, rss)
                samples += 1
            if time.perf_counter() - started > timeout:
                timed_out = True
                proc.kill()
                proc.wait()
                break
            time.sleep(0.1)
        code = proc.wait()
    return {
        "candidate": name,
        "command": cmd,
        "returncode": code,
        "timed_out": timed_out,
        "elapsed_seconds": round(time.perf_counter() - started, 3),
        "sampled_peak_process_tree_rss_bytes": peak if samples else None,
        "memory_samples": samples,
        "output_bytes": output.stat().st_size if output.exists() else None,
        "stdout_log": str(stdout),
        "stderr_log": str(stderr),
    }


def main():
    p = argparse.ArgumentParser(description=__doc__)
    p.add_argument("--input", required=True, type=Path)
    p.add_argument("--first", required=True, type=int)
    p.add_argument("--last", required=True, type=int)
    p.add_argument("--results", required=True, type=Path)
    p.add_argument("--candidates", nargs="+", default=["pdfcpu", "pdq", "qpdf"],
                   choices=["pdfcpu", "pdq", "qpdf"])
    p.add_argument("--pdfcpu-bin", default="pdfcpu")
    p.add_argument("--pdq-bin", default="pdq")
    p.add_argument("--qpdf-bin", default="qpdf")
    p.add_argument("--timeout", type=int, default=600)
    args = p.parse_args()

    original = args.input.resolve(strict=True)
    if not 1 <= args.first <= args.last:
        p.error("range must be 1-based and non-empty")
    if args.timeout < 1:
        p.error("timeout must be positive")
    if args.results.exists():
        p.error("results directory already exists; use a fresh directory")
    binaries = {
        name: shutil.which(getattr(args, name + "_bin"))
        for name in ("pdfcpu", "pdq", "qpdf")
    }
    if binaries["qpdf"] is None:
        p.error("qpdf is required as the structural verifier")
    input_sha = sha256(original)
    n_pages = page_count(binaries["qpdf"], original)
    if args.last > n_pages:
        p.error(f"last page exceeds original page count: {n_pages}")

    args.results.mkdir(parents=True)
    logdir = args.results / "logs"
    logdir.mkdir()
    records = []
    target = args.last - args.first + 1
    for name in dict.fromkeys(args.candidates):
        output = args.results / (name + ".pdf")
        if not binaries[name]:
            records.append({"candidate": name, "status": "unavailable",
                            "reason": "binary not found"})
            continue
        commands = {
            "pdfcpu": [binaries["pdfcpu"], "trim", "--pages",
                       f"{args.first}-{args.last}", str(original), str(output)],
            "pdq": [binaries["pdq"], "split", str(original), "--out",
                    f"{args.first}-{args.last}", str(output)],
            "qpdf": [binaries["qpdf"], "--empty", "--pages", str(original),
                     f"{args.first}-{args.last}", "--", str(output)],
        }
        try:
            record = run_one(name, commands[name], output, args.timeout, logdir)
            record["status"] = "failed"
            if record["returncode"] == 0 and not record["timed_out"] and output.is_file():
                check = capture([binaries["qpdf"], "--check", str(output)])
                count = page_count(binaries["qpdf"], output)
                record["verified_page_count"] = count
                record["qpdf_check_returncode"] = check.returncode
                record["qpdf_check_stderr"] = check.stderr[-2000:]
                if check.returncode == 0 and count == target:
                    record["status"] = "structurally_verified"
                    record["sha256"] = sha256(output)
                    record["page_mapping"] = {
                        "local_first": 1, "local_last": target,
                        "original_first": args.first, "original_last": args.last,
                    }
                else:
                    record["reason"] = "output failed qpdf check or expected page count"
            else:
                record["reason"] = "process failed, timed out or omitted output"
        except (OSError, ValueError, subprocess.TimeoutExpired, RuntimeError) as exc:
            record = {"candidate": name, "status": "failed", "reason": str(exc)}
        records.append(record)
        (args.results / "partial.json").write_text(
            json.dumps(records, indent=2, ensure_ascii=False), encoding="utf-8"
        )
    after = sha256(original)
    report = {
        "schema": "viii.pdf-chunker.v1",
        "created_at_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
        "input": str(original),
        "original_sha256_before": input_sha,
        "original_sha256_after": after,
        "input_unchanged": input_sha == after,
        "original_bytes": original.stat().st_size,
        "total_pages": n_pages,
        "first": args.first,
        "last": args.last,
        "machine": platform.platform(),
        "binary_identities": {name: {"path": path, "sha256": sha256(path)} if path else None
                              for name, path in binaries.items()},
        "python": sys.version,
        "psutil_available": psutil is not None,
        "notes": [
            "Memory samples are lower bounds; include only measured process tree, not all host memory.",
            "Structural validation is not semantic equivalence; OCR/citation proof is a separate gate.",
            "These are native subprocess tests, NOT WASI or Bacalhau compatibility evidence.",
        ],
        "results": records,
    }
    (args.results / "summary.json").write_text(
        json.dumps(report, indent=2, ensure_ascii=False), encoding="utf-8"
    )
    print(json.dumps(report, indent=2, ensure_ascii=False))
    if input_sha != after or any(r["status"] != "structurally_verified" for r in records):
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
