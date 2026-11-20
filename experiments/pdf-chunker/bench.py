#!/usr/bin/env python3
"""Run an isolated, reproducible native PDF page-range extraction comparison.

Native subprocess baselines only: this is NOT WASI or Bacalhau evidence
(see bacalhau_wasi.py for that). Never modifies the input; the results
directory must not exist.

Memory/CPU/I/O per run are read from the operating system for the child
process itself, after it exits, and are exact for that process:
  * Windows: GetProcessMemoryInfo (PeakWorkingSetSize, PeakPagefileUsage),
    GetProcessIoCounters (ReadTransferCount ...) and GetProcessTimes on the
    child handle still held by subprocess.Popen;
  * POSIX: os.wait4 rusage (ru_maxrss, CPU); I/O is unavailable.
A sampled process-tree RSS (lower bound) is kept as a cross-check.
Byte counters include file-cache hits: they count bytes requested, not disk reads.
"""
import argparse
import ctypes
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import shutil
import statistics
import subprocess
import sys
import threading
import time

try:
    import psutil
except ImportError:
    psutil = None

SCHEMA = "viii.pdf-chunker-native.v2"


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def capture(command, timeout=600):
    return subprocess.run(command, text=True, capture_output=True, timeout=timeout)


def page_count(qpdf, path):
    done = capture([qpdf, "--show-npages", str(path)])
    if done.returncode:
        raise RuntimeError(f"qpdf page count: {done.stderr.strip()}")
    return int(done.stdout.strip())


def commands(name, binary, original, first, last, output):
    """Exact CLI syntax verified against pdfcpu v0.16.4, pdq 0.5.1 and qpdf 12.4.2."""
    rng = f"{first}-{last}"
    return {
        "pdfcpu": [binary, "trim", "--pages", rng, str(original), str(output)],
        "pdq": [binary, "split", str(original), "--out", rng, str(output)],
        # --deterministic-id: otherwise the trailer /ID is random and chunk hashes differ per run.
        "qpdf": [binary, "--deterministic-id", "--empty", "--pages", str(original), rng, "--", str(output)],
    }[name]


if os.name == "nt":
    from ctypes import wintypes

    class _MemCounters(ctypes.Structure):
        _fields_ = [("cb", wintypes.DWORD), ("PageFaultCount", wintypes.DWORD),
                    ("PeakWorkingSetSize", ctypes.c_size_t), ("WorkingSetSize", ctypes.c_size_t),
                    ("QuotaPeakPagedPoolUsage", ctypes.c_size_t), ("QuotaPagedPoolUsage", ctypes.c_size_t),
                    ("QuotaPeakNonPagedPoolUsage", ctypes.c_size_t), ("QuotaNonPagedPoolUsage", ctypes.c_size_t),
                    ("PagefileUsage", ctypes.c_size_t), ("PeakPagefileUsage", ctypes.c_size_t)]

    class _IoCounters(ctypes.Structure):
        _fields_ = [(n, ctypes.c_ulonglong) for n in ("ReadOperationCount", "WriteOperationCount", "OtherOperationCount",
                                                      "ReadTransferCount", "WriteTransferCount", "OtherTransferCount")]

    def exact_metrics(proc):
        handle = wintypes.HANDLE(int(proc._handle))
        mem = _MemCounters()
        mem.cb = ctypes.sizeof(mem)
        io = _IoCounters()
        times = [wintypes.FILETIME() for _ in range(4)]
        k32, psapi = ctypes.windll.kernel32, ctypes.windll.psapi
        out = {"source": "win32-handle"}
        if psapi.GetProcessMemoryInfo(handle, ctypes.byref(mem), mem.cb):
            out.update(peak_working_set_bytes=mem.PeakWorkingSetSize, peak_private_bytes=mem.PeakPagefileUsage,
                       page_faults=mem.PageFaultCount)
        if k32.GetProcessIoCounters(handle, ctypes.byref(io)):
            out.update(read_bytes=io.ReadTransferCount, write_bytes=io.WriteTransferCount,
                       read_ops=io.ReadOperationCount, write_ops=io.WriteOperationCount)
        if k32.GetProcessTimes(handle, *[ctypes.byref(t) for t in times]):
            to_s = lambda ft: ((ft.dwHighDateTime << 32) | ft.dwLowDateTime) / 1e7
            out.update(cpu_kernel_s=round(to_s(times[2]), 3), cpu_user_s=round(to_s(times[3]), 3))
        return out

    def wait_exact(proc):
        code = proc.wait()
        return code, exact_metrics(proc)
else:
    def wait_exact(proc):
        _, status, usage = os.wait4(proc.pid, 0)
        proc.returncode = os.waitstatus_to_exitcode(status)
        scale = 1 if sys.platform == "darwin" else 1024  # Linux ru_maxrss is KiB
        return proc.returncode, {"source": "wait4", "peak_rss_bytes": usage.ru_maxrss * scale,
                                 "cpu_user_s": round(usage.ru_utime, 3), "cpu_kernel_s": round(usage.ru_stime, 3)}


def kill_tree(proc):
    if psutil is not None:
        try:
            for child in psutil.Process(proc.pid).children(recursive=True):
                child.kill()
        except psutil.Error:
            pass
    proc.kill()


def run_one(name, cmd, output, timeout, logdir, tag=None, min_available=0):
    """min_available: kill the tree if host available RAM drops below it (bytes)."""
    tag = tag or name
    stdout = logdir / f"{tag}.stdout.log"
    stderr = logdir / f"{tag}.stderr.log"
    state = {"peak": 0, "samples": 0, "timed_out": False, "memory_guard": False, "min_host_available": None}
    started = time.perf_counter()
    with stdout.open("wb") as so, stderr.open("wb") as se:
        proc = subprocess.Popen(cmd, stdout=so, stderr=se)
        done = threading.Event()

        def watch():
            while not done.wait(0.05):
                if psutil is not None:
                    try:
                        root = psutil.Process(proc.pid)
                        rss = sum(p.memory_info().rss for p in [root] + root.children(recursive=True))
                        state["peak"] = max(state["peak"], rss)
                        state["samples"] += 1
                        avail = psutil.virtual_memory().available
                        low = state["min_host_available"]
                        state["min_host_available"] = avail if low is None else min(low, avail)
                        if min_available and avail < min_available and not state["memory_guard"]:
                            state["memory_guard"] = True
                            kill_tree(proc)
                    except psutil.Error:
                        pass
                if time.perf_counter() - started > timeout and not state["timed_out"]:
                    state["timed_out"] = True
                    kill_tree(proc)

        watcher = threading.Thread(target=watch, daemon=True)
        watcher.start()
        code, exact = wait_exact(proc)
        elapsed = time.perf_counter() - started
        done.set()
        watcher.join()
    return {
        "candidate": name, "command": [str(c) for c in cmd], "returncode": code,
        "timed_out": state["timed_out"], "killed_by_memory_guard": state["memory_guard"],
        "min_host_available_bytes": state["min_host_available"],
        "elapsed_seconds": round(elapsed, 3),
        "exact": exact,
        "sampled_peak_process_tree_rss_bytes": state["peak"] if state["samples"] else None,
        "memory_samples": state["samples"],
        "output_bytes": output.stat().st_size if output.exists() else None,
        "stdout_log": str(stdout), "stderr_log": str(stderr),
    }


def tool_version(name, binary):
    flag = {"pdfcpu": ["version"], "qpdf": ["--version"], "pdq": ["--help"]}[name]
    try:
        out = capture([binary, *flag], timeout=30)
        return (out.stdout or out.stderr).strip().splitlines()[0][:200]
    except (OSError, subprocess.SubprocessError, IndexError):
        return None


def main(argv=None):
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--input", required=True, type=Path)
    p.add_argument("--first", required=True, type=int)
    p.add_argument("--last", required=True, type=int)
    p.add_argument("--results", required=True, type=Path)
    p.add_argument("--candidates", nargs="+", default=["pdfcpu", "pdq", "qpdf"], choices=["pdfcpu", "pdq", "qpdf"])
    p.add_argument("--pdfcpu-bin", default="pdfcpu")
    p.add_argument("--pdq-bin", default="pdq")
    p.add_argument("--qpdf-bin", default="qpdf")
    p.add_argument("--timeout", type=int, default=600)
    p.add_argument("--repeat", type=int, default=1, help="runs per candidate (round-robin order)")
    p.add_argument("--validate", action="store_true", help="per-page text/raster comparison with Poppler")
    p.add_argument("--min-available-mb", type=int, default=0,
                   help="abort a run when host available RAM falls below this (protects the workstation)")
    args = p.parse_args(argv)

    original = args.input.resolve(strict=True)
    if not 1 <= args.first <= args.last:
        p.error("range must be 1-based and non-empty")
    if args.timeout < 1 or args.repeat < 1:
        p.error("timeout and repeat must be positive")
    if args.results.exists():
        p.error("results directory already exists; use a fresh directory")
    binaries = {name: shutil.which(getattr(args, name + "_bin")) for name in ("pdfcpu", "pdq", "qpdf")}
    if binaries["qpdf"] is None:
        p.error("qpdf is required as the structural verifier")
    input_sha = sha256(original)
    n_pages = page_count(binaries["qpdf"], original)
    if args.last > n_pages:
        p.error(f"last page exceeds original page count: {n_pages}")

    args.results.mkdir(parents=True)
    logdir = args.results / "logs"
    logdir.mkdir()
    target = args.last - args.first + 1
    records = []
    candidates = list(dict.fromkeys(args.candidates))
    for rep in range(1, args.repeat + 1):
        for name in candidates:
            tag = f"{name}-r{rep}"
            output = args.results / f"{tag}.pdf"
            if not binaries[name]:
                records.append({"candidate": name, "repeat": rep, "status": "unavailable", "reason": "binary not found"})
                continue
            try:
                record = run_one(name, commands(name, binaries[name], original, args.first, args.last, output),
                                 output, args.timeout, logdir, tag, args.min_available_mb * 2**20)
                record.update(repeat=rep, status="failed")
                if record["returncode"] == 0 and not record["timed_out"] and output.is_file():
                    check = capture([binaries["qpdf"], "--check", str(output)])
                    count = page_count(binaries["qpdf"], output)
                    record.update(verified_page_count=count, qpdf_check_returncode=check.returncode,
                                  qpdf_check_tail=check.stdout[-300:] + check.stderr[-300:])
                    if check.returncode in (0, 3) and count == target:  # 3 = warnings only
                        record["status"] = "structurally_verified"
                        record["sha256"] = sha256(output)
                        record["page_mapping"] = {"local_first": 1, "local_last": target,
                                                  "original_first": args.first, "original_last": args.last}
                        if args.validate:
                            import validate
                            record["validation"] = validate.compare_range(original, args.first, args.last, output)
                            if not record["validation"]["all_equal"]:
                                record["status"] = "content_mismatch"
                    else:
                        record["reason"] = "output failed qpdf check or expected page count"
                else:
                    record["reason"] = ("aborted by host memory guard; exact peak is a lower bound"
                                        if record["killed_by_memory_guard"] else
                                        "process failed, timed out or omitted output")
            except (OSError, ValueError, subprocess.SubprocessError, RuntimeError) as exc:
                record = {"candidate": name, "repeat": rep, "status": "failed", "reason": str(exc)}
            records.append(record)
            (args.results / "partial.json").write_text(json.dumps(records, indent=2), encoding="utf-8")
    after = sha256(original)
    aggregate = {}
    for name in candidates:
        ok = [r for r in records if r["candidate"] == name and r.get("status") == "structurally_verified"]
        if ok:
            peak_key = "peak_working_set_bytes" if os.name == "nt" else "peak_rss_bytes"
            aggregate[name] = {
                "runs": len(ok),
                "elapsed_median_s": statistics.median(r["elapsed_seconds"] for r in ok),
                "elapsed_min_s": min(r["elapsed_seconds"] for r in ok),
                "peak_memory_median_bytes": statistics.median(r["exact"].get(peak_key, 0) for r in ok),
                "peak_memory_max_bytes": max(r["exact"].get(peak_key, 0) for r in ok),
                "peak_private_max_bytes": max(r["exact"].get("peak_private_bytes", 0) for r in ok) or None,
                "read_bytes_median": statistics.median(r["exact"]["read_bytes"] for r in ok) if all("read_bytes" in r["exact"] for r in ok) else None,
                "output_bytes": ok[0]["output_bytes"],
                "deterministic_output": len({r["sha256"] for r in ok}) == 1,
            }
    report = {
        "schema": SCHEMA,
        "created_at_utc": dt.datetime.now(dt.timezone.utc).isoformat(),
        "input": str(original), "original_sha256_before": input_sha, "original_sha256_after": after,
        "input_unchanged": input_sha == after, "original_bytes": original.stat().st_size,
        "total_pages": n_pages, "first": args.first, "last": args.last, "repeat": args.repeat,
        "machine": {"platform": platform.platform(), "cpu_count": os.cpu_count(),
                    "total_ram": psutil.virtual_memory().total if psutil else None},
        "binary_identities": {name: {"path": path, "sha256": sha256(path), "version": tool_version(name, path)}
                              if path else None for name, path in binaries.items()},
        "python": sys.version, "psutil_available": psutil is not None,
        "notes": [
            "exact.* figures are OS counters for the child process; sampled RSS is a lower bound.",
            "read_bytes counts bytes requested from the OS, including file-cache hits.",
            "Repeated runs see a warm OS file cache; the first repeat may be colder.",
            "These are native subprocess tests, NOT WASI or Bacalhau compatibility evidence.",
        ],
        "aggregate": aggregate,
        "results": records,
    }
    (args.results / "summary.json").write_text(json.dumps(report, indent=2), encoding="utf-8")
    print(json.dumps({"input_unchanged": report["input_unchanged"], "aggregate": aggregate}, indent=2))
    if input_sha != after or any(r.get("status") != "structurally_verified" for r in records):
        return 1
    return 0


if __name__ == "__main__":
    sys.path.insert(0, str(Path(__file__).resolve().parent))
    raise SystemExit(main())
