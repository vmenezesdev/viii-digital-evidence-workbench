"""Verify archived executor runs and regenerate their comparison figure."""

import argparse
import base64
import gzip
import json
import math
import re
from collections import defaultdict
from datetime import datetime
from pathlib import Path
from statistics import median

import matplotlib

matplotlib.use("Agg")
import matplotlib.pyplot as plt
from opentelemetry.proto.collector.trace.v1.trace_service_pb2 import (
    ExportTraceServiceRequest,
)


PATHS = (
    "linux-in-process", "linux-subprocess", "docker", "wasi-linux",
    "windows-in-process", "windows-subprocess", "wasi-windows",
)
LABELS = dict(zip(PATHS, (
    "Go in-process (Linux)", "Go subprocess (Linux)", "Bacalhau Docker",
    "Bacalhau WASI (Linux)", "Go in-process (Windows)",
    "Go subprocess (Windows)", "Bacalhau WASI (Windows)",
)))


def jsonl(path):
    return [json.loads(line) for line in path.read_text(encoding="utf-8-sig").splitlines() if line.strip()]


def cell(row):
    workload = row["workload"]
    return tuple(workload[name] for name in ("target_ms", "input_bytes", "concurrency"))


def ms(values):
    values = sorted(n / 1e6 for n in values)
    if not values:
        return None
    center = median(values)
    return {
        "n": len(values), "median_ms": center,
        "p95_ms": values[math.ceil(0.95 * len(values)) - 1],
        "mad_ms": median(abs(v - center) for v in values),
    }


def engine_spans(path):
    """Match job IDs through attributes and parent trace context."""
    traces = defaultdict(list)
    source = path.open("rb") if path.exists() else gzip.open(str(path) + ".gz", "rb")
    with source:
        for line in source:
            request = ExportTraceServiceRequest.FromString(base64.b64decode(line))
            for resource in request.resource_spans:
                for scope in resource.scope_spans:
                    for span in scope.spans:
                        traces[span.trace_id].append(span)
    jobs = defaultdict(lambda: defaultdict(list))
    for trace in traces.values():
        ids = {}
        indexed = {span.span_id: span for span in trace}
        for span in trace:
            for attribute in span.attributes:
                if attribute.key == "jobid" and attribute.value.string_value.startswith("j-"):
                    ids[span.span_id] = attribute.value.string_value
        distinct = set(ids.values())
        for span in trace:
            parent = span
            while parent and parent.span_id not in ids:
                parent = indexed.get(parent.parent_span_id)
            job = ids[parent.span_id] if parent else None
            if job is None and len(distinct) == 1:
                job = next(iter(distinct))
            if job:
                name = re.sub(r"/containers/[0-9a-f]{64}/", "/containers/{id}/", span.name)
                jobs[job][name].append((span.start_time_unix_nano, span.end_time_unix_nano))
    return jobs


def interval_duration(intervals):
    """Duration of the union of matching spans, including overlapping downloads once."""
    duration, last_end = 0, 0
    for start, end in sorted(intervals):
        duration += max(0, end - max(start, last_end))
        last_end = max(last_end, end)
    return duration


def docker_events(path):
    by_job = defaultdict(dict)
    for event in jsonl(path):
        attrs = event["Actor"]["Attributes"]
        match = re.search(r"j-[0-9a-f-]{36}", attrs.get("bacalhau-jobID", ""))
        if match:
            action = event["Action"]
            if action in {"create", "start", "die"} and action in by_job[match.group()]:
                raise ValueError(f"Repeated Docker {action} event for {match.group()}")
            by_job[match.group()][action] = (event["timeNano"], attrs)
    return by_job


def sample_time(row):
    return datetime.fromisoformat(row["started"]).timestamp()


def docker_memory(stats):
    value, unit = re.fullmatch(r"([0-9.]+)\s*(B|KiB|MiB|GiB)", stats["MemUsage"].split("/")[0].strip()).groups()
    return float(value) * {"B": 2**-20, "KiB": 2**-10, "MiB": 1, "GiB": 1024}[unit]


def resources(rows, daemon_rows, start=-math.inf, stop=math.inf):
    processes = defaultdict(list)
    for row in rows:
        at = datetime.fromisoformat(row["at"]).timestamp()
        windows = defaultdict(list)
        if start <= at <= stop:
            for proc in row.get("windows", ()):
                windows[proc.get("role") or proc["name"]].append(proc)
        for role, members in windows.items():
            cpu = [p["cpu_seconds"] for p in members]
            processes[role].append((at, sum(cpu) if all(v is not None for v in cpu) else None,
                                    sum(p["working_set_bytes"] for p in members) / 2**20))
        at = datetime.fromisoformat(row["docker_observed_at"]).timestamp()
        for role, stats in [("linux_compute_and_driver", row.get("docker"))] + [
            ("docker_worker", stat) for stat in row.get("docker_workers", ())
        ]:
            if start <= at <= stop and stats and stats["CPUPerc"] != "--" and stats["MemUsage"] != "--":
                memory = docker_memory(stats)
                if role != "docker_worker" or memory > 0:
                    processes[role].append((at, float(stats["CPUPerc"].rstrip("%")), memory))
    for row in daemon_rows:
        at = datetime.fromisoformat(row["at"]).timestamp()
        if start <= at <= stop and "rss_bytes" in row:
            processes[f"dockerd_{row['pid']}"].append((at, row["cpu_seconds"], row["rss_bytes"] / 2**20))
    output = {}
    for role, series in processes.items():
        if role in ("linux_compute_and_driver", "docker_worker"):
            cpu = median(x[1] for x in series)
        elif role != "bench-worker" and series[0][0] < series[-1][0] and all(x[1] is not None for x in series):
            cpu = 100 * (series[-1][1] - series[0][1]) / (series[-1][0] - series[0][0])
        else:
            cpu = None
        output[role] = {
            "snapshots": len(series), "cpu_pct_one_core": cpu,
            "median_mib": median(x[2] for x in series), "peak_observed_mib": max(x[2] for x in series),
        }
    return output


def summarize(root):
    env = json.loads((root / "environment.json").read_text(encoding="utf-8-sig"))
    pull_log = (root / "image-pull.log").read_text(encoding="utf-8-sig")
    spans = engine_spans(root / "otlp-traces.base64")
    events = docker_events(root / "docker-events.jsonl")
    resource_rows = jsonl(root / "resources.jsonl")
    daemon_path = root / "docker-daemon.jsonl"
    daemon_rows = jsonl(daemon_path) if daemon_path.exists() else []
    groups = defaultdict(list)
    summaries = {}
    outputs = defaultdict(set)
    first = {}
    failed = []
    failed_jobs = set()
    docker_jobs = set()
    windows_started = None
    for platform, file in (("linux", root / "latency.jsonl"), ("windows", root / "windows-native/latency.jsonl")):
        rows = jsonl(file)
        if rows[-1].get("event") != "finished":
            raise ValueError(f"Incomplete benchmark: {file}")
        if platform == "windows":
            windows_started = sample_time(rows[0])
        for row in rows:
            if "path" not in row:
                continue
            path = row["path"]
            if path in ("in-process", "subprocess"):
                path = f"{platform}-{path}"
            key = (path, *cell(row))
            if row.get("event") == "summary":
                summaries[key] = row
                continue
            if row.get("error"):
                failed.append(row)
                if row.get("bacalhau", {}).get("job_id"):
                    failed_jobs.add(row["bacalhau"]["job_id"])
                continue
            result = row["result"]
            if row["expected_input_sha256"] != result["input_sha256"]:
                raise ValueError(f"Input mismatch: {row}")
            outputs[cell(row)].add((row["workload"]["rounds"], result["input_sha256"], result["checksum"]))
            job = row.get("bacalhau")
            if job:
                engine, node = ("docker" if path == "docker" else "wasm"), ("viii-a" if path == "wasi-windows" else "viii-b")
                if (job["engine"], job["node_id"], job["state"], job["exit_code"], job["observed_executions"]) != (engine, node, "Completed", 0, 1):
                    raise ValueError(f"Unexpected job identity or status: {job}")
                if env.get("job_cpu") and (job.get("job_cpu"), job.get("job_memory")) != (env["job_cpu"], env["job_memory"]):
                    raise ValueError(f"Unexpected Bacalhau resource request: {job}")
                if path == "docker":
                    docker_jobs.add(job["job_id"])
                    milestones = events.get(job["job_id"], {})
                    if not {"create", "start", "die"} <= milestones.keys():
                        raise ValueError(f"Missing Docker lifecycle events for {job['job_id']}")
                    die = milestones["die"][1]
                    if (die.get("exitCode") != "0" or die.get("image") != env["worker"]["ref"]
                            or not die.get("bacalhau-executionID", "").endswith(job["execution_id"])):
                        raise ValueError(f"Docker event does not match the execution: {job}")
            if row["phase"] == "first-use":
                first[path] = row["elapsed_ns"] / 1e6
            elif row["phase"] == "sample" and not row.get("timing_excluded"):
                groups[key].append(row)
    if any(len(values) != 1 for values in outputs.values()):
        raise ValueError("Executors diverged on a common workload")
    if set(groups) != set(summaries):
        raise ValueError("Missing valid samples for one or more cells")
    if set(events) - docker_jobs - failed_jobs:
        raise ValueError("Docker events include unexpected jobs")
    idle_end = min(sample_time(r) for group in groups.values() for r in group) - 1
    idle = resources(resource_rows, daemon_rows, stop=idle_end)
    windows_idle = resources(resource_rows, daemon_rows, windows_started, windows_started + 9)
    cells = []
    for key, group in groups.items():
        path, target, size, concurrency = key
        timings = defaultdict(list)
        span_hits = defaultdict(list)
        worker_core_percent = []
        for row in group:
            result = row["result"]
            timings["elapsed"].append(row["elapsed_ns"])
            timings["read"].append(result["read_ns"])
            timings["decode"].append(row["decode_ns"])
            timings["compute"].append(result["compute_ns"])
            timings["residual"].append(row["elapsed_ns"] - result["compute_ns"])
            if cpu := result.get("process_cpu_ns") or row.get("subprocess_cpu_ns"):
                timings["worker_cpu"].append(cpu)
            if peak := result.get("process_peak_rss_bytes"):
                timings["worker_rss_bytes"].append(peak)
            if job := row.get("bacalhau"):
                for field in ("submit_ns", "wait_and_fetch_ns", "final_fetch_ns"):
                    timings[field].append(job[field])
                if job.get("job_created_unix_ns") and job.get("execution_created_unix_ns"):
                    timings["to_execution_created"].append(job["execution_created_unix_ns"] - job["job_created_unix_ns"])
                for name, values in spans.get(job["job_id"], {}).items():
                    span_hits[name].append(interval_duration(values))
                if path == "docker":
                    event = events[job["job_id"]]
                    timings["container_create_to_start"].append(event["start"][0] - event["create"][0])
                    lifetime = event["die"][0] - event["start"][0]
                    timings["container_start_to_die"].append(lifetime)
                    if lifetime > 0 and "process_cpu_ns" in result:
                        worker_core_percent.append(100 * result["process_cpu_ns"] / lifetime)
        start = min(sample_time(r) for r in group)
        stop = max(sample_time(r) + r["elapsed_ns"] / 1e9 for r in group)
        rss = timings.pop("worker_rss_bytes", [])
        summary = summaries[key]
        if len(group) != summary["successful"] - summary["timing_excluded"]:
            raise ValueError(f"Sample count differs from driver summary: {key}")
        cells.append({
            "path": path, "target_ms": target, "input_bytes": size, "concurrency": concurrency,
            "success": summary["successful"], "failed": summary["failed"],
            "timing_excluded": summary["timing_excluded"], "jobs_per_second": summary["jobs_per_second"],
            "timings": {name: ms(values) for name, values in timings.items()},
            "span_times": {name: ms(values) for name, values in span_hits.items()},
            "worker_peak_mib": max(rss) / 2**20 if rss else None,
            "worker_cpu_pct_of_lifetime": median(worker_core_percent) if worker_core_percent else None,
            "worker_cgroup_cpu_max": sorted({r["result"]["process_cpu_quota"] for r in group
                                             if "process_cpu_quota" in r["result"]}),
            "resources": resources(resource_rows, daemon_rows, start, stop),
        })
    return {
        "environment": env, "cells": cells, "first_use_ms": first, "idle": idle, "failures": failed,
        "windows_native_driver_idle": windows_idle.get("bench-worker"),
        "docker_layer_downloaded": ": Download complete" in pull_log,
    }


def plot(report, path, fresh_pull_ms=None):
    plt.style.use("seaborn-v0_8-whitegrid")
    fig, axes = plt.subplots(2, 3, figsize=(15, 8), layout="constrained")
    latency, compute, throughput, ratio, startup, resource = axes.flat
    serial = [c for c in report["cells"] if c["concurrency"] == 1 and c["input_bytes"] == 10240]
    native = {c["target_ms"]: c["timings"]["elapsed"]["median_ms"] for c in serial if c["path"] == "linux-subprocess"}
    for i, executor in enumerate(PATHS):
        color = f"C{i}"
        rows = sorted((c for c in serial if c["path"] == executor), key=lambda c: c["target_ms"])
        if not rows:
            continue
        elapsed = [c["timings"]["elapsed"]["median_ms"] for c in rows]
        work = [c["timings"]["compute"]["median_ms"] for c in rows]
        x = [c["target_ms"] for c in rows]
        latency.loglog(x, elapsed, "o-", color=color, label=LABELS[executor])
        compute.loglog(work, elapsed, "o-", color=color)
        ratio.loglog(x, [e / native[t] for e, t in zip(elapsed, x)], "o-", color=color)
        scaling = sorted((c for c in report["cells"] if c["path"] == executor and c["target_ms"] == 100 and c["input_bytes"] == 10240), key=lambda c: c["concurrency"])
        throughput.plot([c["concurrency"] for c in scaling], [c["jobs_per_second"] for c in scaling], "o-", color=color)
        startup.barh(i - .17, report["first_use_ms"][executor], .32, color=color, alpha=.45)
        startup.barh(i + .17, elapsed[0], .32, color=color)
    startup.barh(len(PATHS), report["environment"]["cold_pull"]["elapsed_ms"], .32, color="0.4")
    pull_label = "Docker image pull" if report["docker_layer_downloaded"] else "Docker pull (cached layer)"
    startup_labels = [LABELS[p] for p in PATHS] + [pull_label]
    if fresh_pull_ms is not None:
        startup.barh(len(startup_labels), fresh_pull_ms, .32, color="0.6")
        startup_labels.append("Docker pull (fresh layer)")
    startup.set(yticks=range(len(startup_labels)), yticklabels=startup_labels, xscale="log")
    startup.invert_yaxis()
    compute.plot([.3, 1e5], [.3, 1e5], "--", color=".65", linewidth=1)
    throughput.set(xticks=[1, 2, 4])
    if any(line.get_ydata().size for line in throughput.lines):
        throughput.set_yscale("log")
    else:
        throughput.set_axis_off()
    ratio.axhline(1, color=".65", linestyle="--")
    for ax, title, xlabel, ylabel in [
        (latency, "Serial end-to-end latency", "WASI calibration target (ms)", "Median wall time (ms)"),
        (compute, "Worker compute versus wall time", "Median compute time (ms)", "Median wall time (ms)"),
        (throughput, "Batch throughput at the 100 ms target", "Concurrent jobs", "Jobs per second"),
        (ratio, "End-to-end ratio against Linux Go subprocess", "WASI calibration target (ms)", "Wall-time ratio"),
        (startup, "First use (lighter bar) and warm median", "Wall time (ms)", ""),
    ]:
        ax.set(title=title, xlabel=xlabel, ylabel=ylabel)
    role_labels = {
        "stage": "HTTP staging", "orchestrator": "Orchestrator",
        "windows_compute": "Windows compute", "linux_compute_and_driver": "Linux compute + driver",
        "com.docker.backend": "Docker Desktop backend", "vmmemWSL": "WSL VM",
    }
    bars = [(role_labels.get(k, "Docker daemon"), v) for k, v in report["idle"].items()
            if k in role_labels or k.startswith("dockerd_")]
    resource.barh([r for r, _ in bars], [v["median_mib"] for _, v in bars], color="C0", alpha=.65)
    resource.set(title="Cluster-ready idle footprint", xlabel="Median sampled memory (MiB)")
    for i, (_, values) in enumerate(bars):
        if (cpu := values["cpu_pct_one_core"]) is not None:
            resource.text(values["median_mib"], i, f" {cpu:.1f}% CPU", va="center", fontsize=8)
    resource.margins(x=.25)
    latency.legend(fontsize=7)
    fig.suptitle("Native Go, Bacalhau WASI and Bacalhau Docker on one Windows workstation", fontsize=15)
    fig.savefig(path, dpi=160)
    plt.close(fig)


def plot_scaling(report, path):
    if not any(c["concurrency"] > 1 for c in report["cells"]):
        return
    fig, (cpu, memory) = plt.subplots(1, 2, figsize=(11, 4.5), layout="constrained")
    scopes = [
        ("wasi-linux", "linux_compute_and_driver", "WASI Linux node + driver"),
        ("docker", "linux_compute_and_driver", "Docker Linux node + driver"),
        ("wasi-windows", "windows_compute", "WASI Windows compute"),
    ]
    for executor, role, label in scopes:
        cells = sorted((c for c in report["cells"] if c["path"] == executor and c["target_ms"] == 100
                        and c["input_bytes"] == 10240 and role in c["resources"]), key=lambda c: c["concurrency"])
        if not cells:
            continue
        x = [c["concurrency"] for c in cells]
        metrics = [c["resources"][role] for c in cells]
        cpu.plot(x, [m["cpu_pct_one_core"] for m in metrics], "o-", label=label)
        memory.plot(x, [m["peak_observed_mib"] for m in metrics], "o-", label=label)
    cells = sorted((c for c in report["cells"] if c["path"] == "docker" and c["target_ms"] == 100
                    and c["input_bytes"] == 10240), key=lambda c: c["concurrency"])
    cpu.plot([c["concurrency"] for c in cells], [c["worker_cpu_pct_of_lifetime"] for c in cells], "x--",
             label="Docker worker CPU / lifetime")
    memory.plot([c["concurrency"] for c in cells], [c["worker_peak_mib"] for c in cells], "x--",
                label="Docker max worker RSS")
    for ax in (cpu, memory):
        ax.set(xlabel="Concurrent jobs", xticks=[1, 2, 4])
    cpu.set(ylabel="Percent of one logical core", title="CPU during measured batches")
    memory.set(ylabel="MiB", title="Peak observed memory during measured batches")
    if memory.lines:
        memory.legend(fontsize=8)
        cpu.legend(fontsize=8)
    fig.savefig(path, dpi=160)
    plt.close(fig)


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("results", type=Path)
    parser.add_argument("output", type=Path)
    parser.add_argument("--cold-control", type=Path, help="summary.json from a verified fresh-layer run")
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=True)
    report = summarize(args.results)
    fresh_pull_ms = None
    if args.cold_control:
        control = json.loads(args.cold_control.read_text(encoding="utf-8-sig"))
        if not control["docker_layer_downloaded"]:
            raise ValueError("The cold control requires a verified layer download")
        fresh_pull_ms = control["environment"]["cold_pull"]["elapsed_ms"]
    (args.output / "summary.json").write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    plot(report, args.output / "executors.png", fresh_pull_ms)
    plot_scaling(report, args.output / "executor-resources.png")
