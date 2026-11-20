#!/usr/bin/env python3
"""Run WASI modules as real jobs on an isolated, pinned Bacalhau node.

The runner owns everything it touches: a fresh results directory, a node it
starts on free loopback ports with Docker disabled, and only that node's PID.
It never connects to, stops or reuses another Bacalhau node or results dir.

Plan (JSON):
  {
    "module": "path/to/module.wasm",
    "mounts": {"/original.pdf": "path/to/original.pdf"},   # read-only
    "jobs": [{"id": "r1", "args": ["/original.pdf", "1", "4", "/out", "/control/r1.ack"]}],
    "ready_protocol": true,      # wait for out/ready, verify, then write the ack
    "memory": "3gb", "cpu": "1", "timeout_s": 900,
    "leak_markers": ["JABUTICABA"]   # must never appear in job stdout/stderr
  }

Memory: on Windows the node's OS-tracked peak working set and peak private
bytes (peak_wset / peak_pagefile) are exact for the node process lifetime.
WASM modules execute inside the node (wazero), so the node peak includes the
module's linear memory *and* the node's own baseline, which is recorded before
submission. A sampled timeline is kept as well, labelled as sampled.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import socket
import subprocess
import sys
import threading
import time
import urllib.error
import urllib.request

try:
    import psutil
except ImportError:  # pragma: no cover - reported in metadata
    psutil = None

sys.path.insert(0, str(Path(__file__).resolve().parent))
from bench import exact_metrics  # noqa: E402  (Windows: exact peaks from the process handle)

PINNED_VERSION = "v1.9.5"
PINNED_REVISION = "3a6a73aa6529755ac2c9d96a771f0566ff3ee5da"
# Bacalhau v1.9.5 models.ExecutionStateType, observed: 9 Completed, 10 Failed (11 Cancelled).
EXECUTION_COMPLETED = 9
TERMINAL = {"Completed", "Failed", "Stopped", "Cancelled", "Canceled"}
SCHEMA = "viii.bacalhau-wasi-run.v1"


def sha256(path):
    digest = hashlib.sha256()
    with open(path, "rb") as stream:
        for block in iter(lambda: stream.read(1 << 20), b""):
            digest.update(block)
    return digest.hexdigest()


def identity(path):
    """sha256 of a file, or of a directory tree as sorted 'relpath NUL sha256 LF' lines."""
    path = Path(path)
    if path.is_file():
        return sha256(path)
    digest = hashlib.sha256()
    for f in sorted(p for p in path.rglob("*") if p.is_file()):
        digest.update(f"{f.relative_to(path).as_posix()}\0{sha256(f)}\n".encode())
    return "tree:" + digest.hexdigest()


def verify_pin(go_version_m_output):
    """True if `go version -m bacalhau` shows the pinned module and revision."""
    text = go_version_m_output
    return (re.search(r"mod\s+github\.com/bacalhau-project/bacalhau\s+" + re.escape(PINNED_VERSION) + r"\b", text) is not None
            and f"vcs.revision={PINNED_REVISION}" in text)


def free_port():
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as s:
        s.bind(("127.0.0.1", 0))
        return s.getsockname()[1]


def slash(path):
    return str(path).replace("\\", "/")


def node_config(api_port, nats_port, token, allow):
    return {
        "DisableAnalytics": True,
        "API": {"Host": "127.0.0.1", "Port": api_port},
        "Orchestrator": {"Enabled": True, "Host": "127.0.0.1", "Port": nats_port, "Auth": {"Token": token}},
        "Compute": {"Enabled": True, "Orchestrators": [f"nats://127.0.0.1:{nats_port}"],
                    "Auth": {"Token": token}, "AllowListedLocalPaths": [slash(p) + ":ro" for p in allow]},
        "Engines": {"Disabled": ["docker"]},
        "Publishers": {"Disabled": ["local"]},
    }


def local_input(source, target):
    return {"Source": {"Type": "local", "Params": {"SourcePath": slash(source), "ReadWrite": False}}, "Target": target}


def job_spec(name, node, module, mounts, control, args, memory, cpu, timeout_s):
    inputs = [local_input(module, "/worker")]
    inputs += [local_input(src, target) for target, src in sorted(mounts.items())]
    inputs.append(local_input(control, "/control"))
    return {"Job": {
        "Name": name, "Type": "batch", "Count": 1,
        "Constraints": [{"Key": "name", "Operator": "in", "Values": [node]}],
        "Tasks": [{
            "Name": "main",
            "Engine": {"Type": "wasm", "Params": {"EntryModule": "/worker", "Entrypoint": "_start", "Parameters": list(args)}},
            "InputSources": inputs,
            # v1.9.5 validates Path with Windows filepath.IsAbs; WASI mounts Name at /out.
            "ResultPaths": [{"Name": "out", "Path": "C:/out"}],
            "Publisher": {"Type": "noop"},
            "Network": {"Type": "none"},
            "Resources": {"CPU": cpu, "Memory": memory},
            "Timeouts": {"TotalTimeout": timeout_s},
        }],
    }}


def find_leaks(text, markers):
    return sorted({m for m in markers if m and m in (text or "")})


def ready_marker(out_dir):
    """A ready marker counts only with content "ready": Bacalhau v1.9.5's WASI
    /out mount creates an empty file when a guest merely stats a missing path."""
    try:
        return (Path(out_dir) / "ready").read_bytes() == b"ready"
    except OSError:
        return False


def verify_chunk_dir(out_dir, expect_first=None, expect_last=None):
    """Validate a chunker output directory; returns (ok, reason, manifest)."""
    out_dir = Path(out_dir)
    if not ready_marker(out_dir):
        return False, "ready marker missing or empty", None
    chunk = out_dir / "chunk.pdf"
    manifest_path = out_dir / "manifest.json"
    if not chunk.is_file() or not manifest_path.is_file():
        return False, "missing chunk.pdf or manifest.json", None
    try:
        manifest = json.loads(manifest_path.read_text(encoding="utf-8"))
    except ValueError as exc:
        return False, f"manifest is not JSON: {exc}", None
    if manifest.get("chunk_sha256") != sha256(chunk):
        return False, "manifest chunk_sha256 differs from chunk.pdf", manifest
    if expect_first is not None and (manifest.get("physical_first"), manifest.get("physical_last")) != (expect_first, expect_last):
        return False, "manifest physical range differs from request", manifest
    pages = manifest.get("pages") or []
    want = [{"local": i + 1, "physical": p} for i, p in enumerate(range(manifest.get("physical_first", 0), manifest.get("physical_last", -1) + 1))]
    if pages != want or manifest.get("chunk_page_count") != len(want):
        return False, "local/physical page mapping is not contiguous and exact", manifest
    return True, "ok", manifest


def verify_pages_dir(out_dir, first, last):
    """Validate a workers/pdf output directory: ready plus one JSON per physical page."""
    out_dir = Path(out_dir)
    if not ready_marker(out_dir):
        return False, "ready marker missing or empty", None
    statuses = {}
    for number in range(first, last + 1):
        path = out_dir / f"page-{number:06d}.json"
        try:
            page = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, ValueError):
            return False, f"missing or invalid page-{number:06d}.json", None
        if page.get("page") != number:
            return False, f"page-{number:06d}.json reports page {page.get('page')}", None
        statuses[number] = {"status": page.get("status"), "source": page.get("source"),
                            "tool": page.get("tool"), "chars": len((page.get("text") or "").strip())}
    return True, "ok", {"pages": statuses}


def dir_bytes(path):
    total = 0
    for root, _, files in os.walk(path):
        for f in files:
            try:
                total += os.path.getsize(os.path.join(root, f))
            except OSError:
                pass
    return total


class Api:
    def __init__(self, port):
        self.base = f"http://127.0.0.1:{port}/api/v1/orchestrator"

    def call(self, method, path, body=None, timeout=10):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(self.base + path, data=data, method=method,
                                     headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(req, timeout=timeout) as resp:
            raw = resp.read()
        return json.loads(raw) if raw else None


class Sampler(threading.Thread):
    """Samples node-tree working set and host memory; sampled, not exact."""

    def __init__(self, pid, path, interval=0.2, min_available=0, on_low_memory=None):
        super().__init__(daemon=True)
        self.pid, self.path, self.interval = pid, path, interval
        self.min_available, self.on_low_memory, self.guard_tripped = min_available, on_low_memory, False
        self.stop_event = threading.Event()
        self.peak_tree_rss = 0
        self.min_available = None
        self.samples = 0

    def run(self):
        if psutil is None:
            return
        with open(self.path, "w", encoding="utf-8") as log:
            t0 = time.perf_counter()
            while not self.stop_event.is_set():
                try:
                    root = psutil.Process(self.pid)
                    procs = [root] + root.children(recursive=True)
                    rss = sum(p.memory_info().rss for p in procs)
                    cpu = root.cpu_times()
                except (psutil.NoSuchProcess, psutil.AccessDenied):
                    break
                vm = psutil.virtual_memory()
                self.peak_tree_rss = max(self.peak_tree_rss, rss)
                self.min_available = vm.available if self.min_available is None else min(self.min_available, vm.available)
                self.samples += 1
                if self.min_available and vm.available < self.min_available and not self.guard_tripped:
                    self.guard_tripped = True
                    if self.on_low_memory:
                        self.on_low_memory()
                log.write(json.dumps({"t": round(time.perf_counter() - t0, 3), "tree_rss": rss,
                                      "host_available": vm.available, "cpu_user": cpu.user,
                                      "cpu_system": cpu.system}) + "\n")
                self.stop_event.wait(self.interval)


def process_snapshot(proc):
    if psutil is None:
        return {}
    p = psutil.Process(proc.pid)
    mem = p.memory_info()
    snap = {"rss": mem.rss, "cpu_user_s": p.cpu_times().user, "cpu_system_s": p.cpu_times().system}
    for field in ("peak_wset", "peak_pagefile", "private", "pagefile"):
        if hasattr(mem, field):
            snap[field] = getattr(mem, field)
    try:
        io = p.io_counters()
        snap.update(read_bytes=io.read_bytes, write_bytes=io.write_bytes,
                    read_count=io.read_count, write_count=io.write_count)
    except (AttributeError, psutil.AccessDenied):
        pass
    return snap


def machine_metadata():
    meta = {"platform": platform.platform(), "machine": platform.machine(), "python": sys.version,
            "cpu_count_logical": os.cpu_count(), "psutil": getattr(psutil, "__version__", None)}
    if psutil is not None:
        vm = psutil.virtual_memory()
        meta.update(total_ram=vm.total, available_ram_at_start=vm.available,
                    cpu_count_physical=psutil.cpu_count(logical=False))
    return meta


def other_bacalhau_processes(own_exe):
    """Return descriptions of Bacalhau processes we do not own (contention check)."""
    found = []
    if psutil is None:
        return found
    for p in psutil.process_iter(["pid", "name", "exe", "create_time"]):
        if (p.info.get("name") or "").lower().startswith("bacalhau"):
            found.append({"pid": p.info["pid"], "exe": p.info.get("exe")})
    return found


def run(plan, results, bacalhau, poll=0.1):
    started_utc = dt.datetime.now(dt.timezone.utc).isoformat()
    module = Path(plan["module"]).resolve(strict=True)
    mounts = {t: Path(s).resolve(strict=True) for t, s in plan.get("mounts", {}).items()}
    jobs = plan["jobs"]
    if len({j["id"] for j in jobs}) != len(jobs) or not all(re.fullmatch(r"[a-z0-9-]{1,40}", j["id"]) for j in jobs):
        raise SystemExit("job ids must be unique [a-z0-9-]{1,40}")
    results.mkdir(parents=True)
    control = results / "control"
    control.mkdir()
    artifacts = results / "artifacts"
    artifacts.mkdir()
    version_m = subprocess.run(["go", "version", "-m", str(bacalhau)], capture_output=True, text=True)
    if version_m.returncode or not verify_pin(version_m.stdout):
        raise SystemExit("Bacalhau binary does not match the pinned VIII v1.9.5 revision")
    contention = other_bacalhau_processes(bacalhau)
    before = {t: identity(p) for t, p in mounts.items()}
    identities = {"module": {"path": str(module), "sha256": sha256(module), "bytes": module.stat().st_size},
                  "bacalhau": {"path": str(bacalhau), "sha256": sha256(bacalhau),
                               "version": PINNED_VERSION, "revision": PINNED_REVISION}}
    api_port, nats_port = free_port(), free_port()
    node_name = "viii-chunker-" + secrets.token_hex(4)
    config = node_config(api_port, nats_port, secrets.token_hex(16),
                         [module, *mounts.values(), control])
    (results / "node.yaml").write_text(json.dumps(config, indent=2), encoding="utf-8")
    api = Api(api_port)
    node = None
    sampler = None
    records = {j["id"]: {"id": j["id"], "args": j["args"], "acknowledged": False} for j in jobs}
    summary = {"schema": SCHEMA, "started_utc": started_utc, "results": str(results), "node": node_name,
               "plan": plan, "identities": identities, "machine": machine_metadata(),
               "foreign_bacalhau_processes_at_start": contention, "docker_engine_disabled": True,
               "network": "none", "mounts_read_only": True}
    try:
        with open(results / "node.stdout.log", "wb") as so, open(results / "node.stderr.log", "wb") as se:
            flags = getattr(subprocess, "CREATE_NEW_PROCESS_GROUP", 0)
            node = subprocess.Popen([str(bacalhau), "serve", "--name", node_name, "--data-dir", "node", "-c", "node.yaml"],
                                    cwd=results, stdout=so, stderr=se, creationflags=flags)
        (results / "owned.json").write_text(json.dumps({"pid": node.pid, "node": node_name, "api_port": api_port,
                                                        "nats_port": nats_port}), encoding="utf-8")
        t_node = time.perf_counter()
        deadline = time.time() + 60
        while True:
            if node.poll() is not None:
                raise RuntimeError("node exited during startup; see node.stderr.log")
            try:
                nodes = api.call("GET", "/nodes", timeout=3)
                if any(n.get("Info", {}).get("NodeID") == node_name and
                       n.get("Connection") in (None, "CONNECTED") for n in nodes.get("Nodes") or []):
                    break
            except (urllib.error.URLError, OSError, ValueError):
                pass
            if time.time() > deadline:
                raise RuntimeError("node not ready in 60s")
            time.sleep(0.25)
        summary["node_ready_s"] = round(time.perf_counter() - t_node, 3)
        time.sleep(2)  # let the idle node settle before taking its baseline
        summary["node_baseline"] = process_snapshot(node)
        def abort_for_memory():
            # Protect the workstation: stop the owned node; peaks become lower bounds.
            node.kill()

        sampler = Sampler(node.pid, results / "timeline.jsonl",
                          min_available=plan.get("min_available_mb", 0) * 2**20, on_low_memory=abort_for_memory)
        sampler.start()
        t0 = time.perf_counter()
        for j in jobs:
            spec = job_spec(f"viii-chunk-{j['id']}-{secrets.token_hex(3)}", node_name, module, mounts, control,
                            j["args"], plan.get("memory", "3gb"), plan.get("cpu", "1"), plan.get("timeout_s", 900))
            (results / f"job-{j['id']}.json").write_text(json.dumps(spec, indent=2), encoding="utf-8")
            submitted = api.call("PUT", "/jobs", spec, timeout=30)
            records[j["id"]].update(job_id=submitted["JobID"], submitted_s=round(time.perf_counter() - t0, 3))
        deadline = time.time() + plan.get("timeout_s", 900) + 30
        pending = set(records)
        while pending and time.time() < deadline and node.poll() is None:
            for jid in sorted(pending):
                rec = records[jid]
                desc = api.call("GET", f"/jobs/{rec['job_id']}?include=executions")
                items = (desc.get("Executions") or {}).get("Items") or []
                # Bacalhau retries failed executions; keep the whole attempt history.
                rec["executions"] = [{"id": x.get("ID"), "compute_state": x.get("ComputeState"),
                                      "exit_code": (x.get("RunOutput") or {}).get("ExitCode")} for x in items]
                for item in items:
                    if not item.get("ID"):
                        continue
                    out = results / "node" / "compute" / "executions" / "output" / item["ID"] / "results" / "out"
                    if plan.get("ready_protocol") and not rec["acknowledged"] and ready_marker(out):
                        rec["execution_id"] = item["ID"]
                        rec["ready_s"] = round(time.perf_counter() - t0, 3)
                        dest = artifacts / jid
                        if dest.exists():
                            shutil.rmtree(dest)
                        shutil.copytree(out, dest)
                        first, last = int(rec["args"][1]), int(rec["args"][2])
                        if plan.get("verify", "chunk") == "pages":
                            ok, reason, manifest = verify_pages_dir(dest, first, last)
                        else:
                            ok, reason, manifest = verify_chunk_dir(dest, first, last)
                        rec.update(verified=ok, verify_reason=reason, manifest=manifest)
                        if ok:  # acknowledge only verified, copied artifacts
                            ack = control / f"{jid}.ack"
                            tmp = control / f".{jid}.ack.tmp"
                            tmp.write_text("accepted", encoding="utf-8")
                            os.replace(tmp, ack)
                            rec["acknowledged"] = True
                state = desc["Job"]["State"]["StateType"]
                if state in TERMINAL:
                    rec["terminal_s"] = round(time.perf_counter() - t0, 3)
                    rec["state"] = state
                    rec["state_message"] = desc["Job"]["State"].get("Message")
                    if items:
                        final = next((x for x in items if x.get("ID") == rec.get("execution_id")), items[-1])
                        run_output = final.get("RunOutput") or {}
                        rec["exit_code"] = run_output.get("ExitCode")
                        rec["compute_state"] = final.get("ComputeState")
                        rec["attempts"] = len(items)
                        rec["stdout"] = run_output.get("Stdout")
                        rec["stderr"] = run_output.get("stderr") or run_output.get("Stderr")
                        rec["leaks"] = find_leaks((rec["stdout"] or "") + (rec["stderr"] or ""),
                                                  plan.get("leak_markers", []))
                    (results / f"description-{jid}.json").write_text(json.dumps(desc, indent=2), encoding="utf-8")
                    pending.discard(jid)
            time.sleep(poll)
        for jid in pending:
            records[jid]["state"] = "node-killed-by-memory-guard" if node.poll() is not None else "runner-timeout"
        summary["wall_s"] = round(time.perf_counter() - t0, 3)
        summary["node_final"] = process_snapshot(node) if node.poll() is None else None
    finally:
        if sampler:
            sampler.stop_event.set()
            sampler.join(5)
            summary["sampled_peak_tree_rss"] = sampler.peak_tree_rss
            summary["sampled_min_host_available"] = sampler.min_available
            summary["samples"] = sampler.samples
            summary["killed_by_memory_guard"] = sampler.guard_tripped
        for rec in records.values():
            if rec.get("job_id") and rec.get("state") not in TERMINAL:
                try:
                    api.call("DELETE", f"/jobs/{rec['job_id']}", timeout=5)
                except (urllib.error.URLError, OSError, ValueError):
                    pass
        if node and node.poll() is None:
            node.terminate()
            try:
                node.wait(20)
            except subprocess.TimeoutExpired:
                node.kill()
                node.wait()
        if node and os.name == "nt":
            # Lifetime peaks of the node process, readable after exit through the handle.
            summary["node_exact_lifetime"] = exact_metrics(node)
    after = {t: identity(p) for t, p in mounts.items()}
    summary.update(jobs=list(records.values()), mounts_sha256_before=before, mounts_sha256_after=after,
                   mounts_unchanged=before == after, node_data_bytes=dir_bytes(results / "node"),
                   artifact_bytes=dir_bytes(artifacts), finished_utc=dt.datetime.now(dt.timezone.utc).isoformat())
    if summary.get("node_baseline") and summary.get("node_final"):
        b, f = summary["node_baseline"], summary["node_final"]
        if "read_bytes" in f and "read_bytes" in b:
            summary["node_read_bytes_during_jobs"] = f["read_bytes"] - b["read_bytes"]
            summary["node_write_bytes_during_jobs"] = f["write_bytes"] - b["write_bytes"]
        summary["node_cpu_s_during_jobs"] = round((f["cpu_user_s"] + f["cpu_system_s"]) - (b["cpu_user_s"] + b["cpu_system_s"]), 3)
    life = summary.get("node_exact_lifetime") or {}
    if summary.get("node_baseline") and "peak_working_set_bytes" in life:
        b = summary["node_baseline"]
        summary["node_peak_wset_bytes"] = life["peak_working_set_bytes"]
        summary["node_peak_private_bytes"] = life.get("peak_private_bytes")
        summary["node_baseline_rss_bytes"] = b["rss"]
        summary["node_baseline_private_bytes"] = b.get("private")
        summary["node_peak_wset_minus_baseline_rss"] = life["peak_working_set_bytes"] - b["rss"]
        if b.get("private") is not None and life.get("peak_private_bytes") is not None:
            summary["node_peak_private_minus_baseline"] = life["peak_private_bytes"] - b["private"]
    jobs_ok = all(r.get("state") == "Completed" and r.get("exit_code") == 0 and not r.get("leaks")
                  and (r.get("compute_state") or {}).get("StateType") == EXECUTION_COMPLETED
                  and (r.get("acknowledged") or not plan.get("ready_protocol")) for r in records.values())
    summary["passed"] = bool(jobs_ok and summary["mounts_unchanged"])
    (results / "summary.json").write_text(json.dumps(summary, indent=2), encoding="utf-8")
    return summary


def main():
    p = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    p.add_argument("--plan", required=True, type=Path)
    p.add_argument("--results", required=True, type=Path)
    p.add_argument("--bacalhau", type=Path, default=Path(r"C:\Bacalhau\bin\bacalhau.exe"))
    args = p.parse_args()
    if args.results.exists():
        p.error("results directory already exists; use a fresh directory")
    plan = json.loads(args.plan.read_text(encoding="utf-8"))
    summary = run(plan, args.results.resolve(), args.bacalhau.resolve(strict=True))
    brief = {k: summary.get(k) for k in ("passed", "wall_s", "mounts_unchanged", "killed_by_memory_guard",
                                         "node_peak_wset_minus_baseline_rss", "node_peak_private_minus_baseline",
                                         "node_read_bytes_during_jobs", "node_cpu_s_during_jobs")}
    brief["jobs"] = [{k: r.get(k) for k in ("id", "state", "exit_code", "ready_s", "terminal_s", "verified",
                                           "verify_reason", "leaks")} for r in summary["jobs"]]
    print(json.dumps(brief, indent=2))
    return 0 if summary["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
