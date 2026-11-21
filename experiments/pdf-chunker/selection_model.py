#!/usr/bin/env python3
"""Experimental cost model and selection policy for batch PDF chunking
(BATCH-RESULTS.md). NOT a production selector.

Every coefficient is fitted from runs recorded in results/batch.json (see
`fit` in batch_summarize.py); the defaults below are the fitted values written
there and are only used before a fit exists (admission checks in
batch_bench.py). Inputs that the model uses: PDF bytes, page count, chunk count,
memory available on the node, memory reserved for other jobs, a safety margin
and the wasm32 linear-memory ceiling.

  estimate(strategy, pdf_bytes, pages)        -> {"memory": bytes, "time_s": s}
  choose(pdf_bytes, pages, available, ...)    -> decision record
"""
import json
from pathlib import Path

MiB = 1 << 20
WASM32_LIMIT = 4 << 30  # wasm32 linear memory ceiling

# Conservative pre-fit defaults (memory = a + b*bytes + c*pages; time likewise).
# Sources: RESULTS.md / ALTERNATIVES.md single-range and pdq batch runs.
DEFAULTS = {
    # pdq WASI batch: node peak ~2.3x the file (2,737 MiB at 1,193 MiB), 26.2 s ready at 30k.
    "pdq-wasi": {"mem": [40 * MiB, 2.35, 0.0], "time": [1.0, 0.0, 0.00085], "linear": [0, 1.0, 0.0]},
    # flpdf WASI: 161 MiB single range at 30k pre-sized; batch growth unknown -> 2x margin.
    "flpdf-wasi": {"mem": [100 * MiB, 0.0, 2 * 5 * 1024], "time": [3.0, 0.0, 0.01], "linear": [16 * MiB, 0.0, 5 * 1024]},
    # Native (reference only, never a production executor).
    "pdq-native": {"mem": [20 * MiB, 0.05, 0.0], "time": [0.1, 0.0, 0.0001], "linear": [0, 0.0, 0.0]},
    "flpdf-native": {"mem": [10 * MiB, 0.0, 5 * 1024], "time": [0.1, 0.0, 0.005], "linear": [0, 0.0, 0.0]},
}


def load(path=None):
    """Fitted coefficients from results/batch.json, else the defaults."""
    path = Path(path or Path(__file__).resolve().parent / "results" / "batch.json")
    try:
        fitted = json.loads(path.read_text(encoding="utf-8"))["model"]["coefficients"]
        return DEFAULTS | fitted
    except (OSError, ValueError, KeyError):
        return dict(DEFAULTS)


def _lin(c, pdf_bytes, pages):
    return c[0] + c[1] * pdf_bytes + c[2] * pages


def estimate(strategy, pdf_bytes, pages, coeffs=None):
    c = (coeffs or DEFAULTS)[strategy]
    return {"memory": int(_lin(c["mem"], pdf_bytes, pages)), "time_s": round(_lin(c["time"], pdf_bytes, pages), 2),
            "linear": int(_lin(c["linear"], pdf_bytes, pages))}


def choose(pdf_bytes, pages, node_available, reserved=0, margin=1024 * MiB, job_limit=None,
           min_speedup_s=30.0, min_speedup_ratio=1.5, coeffs=None):
    """Policy under test (task section 7):

        if pdq_is_safe and pdq_has_meaningful_speed_advantage: pdq-wasi
        elif flpdf_is_safe: flpdf-wasi
        else: reject (needs more resources)

    safe     = estimated node memory + margin <= available - reserved (and <= the
               job limit if given) and estimated linear memory < 4 GiB.
    meaningful = pdq saves at least `min_speedup_s` seconds AND is at least
               `min_speedup_ratio` times faster.
    """
    coeffs = coeffs or DEFAULTS
    budget = node_available - reserved
    out = {"pdf_bytes": pdf_bytes, "pages": pages, "budget": budget, "margin": margin}
    for s in ("pdq-wasi", "flpdf-wasi"):
        e = estimate(s, pdf_bytes, pages, coeffs)
        safe = (e["memory"] + margin <= budget and e["linear"] < WASM32_LIMIT
                and (job_limit is None or e["memory"] <= job_limit))
        out[s] = e | {"safe": safe}
    p, f = out["pdq-wasi"], out["flpdf-wasi"]
    saved = f["time_s"] - p["time_s"]
    meaningful = saved >= min_speedup_s and f["time_s"] >= min_speedup_ratio * max(p["time_s"], 1e-9)
    out["pdq_speed_advantage_s"] = round(saved, 2)
    out["pdq_meaningful"] = meaningful
    if p["safe"] and meaningful:
        out["choice"] = "pdq-wasi"
    elif f["safe"]:
        out["choice"] = "flpdf-wasi"
    elif p["safe"]:
        out["choice"] = "pdq-wasi"  # flpdf unsafe but pdq fits (many light pages)
    else:
        out["choice"] = "reject"
    return out


if __name__ == "__main__":
    import argparse
    ap = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter)
    ap.add_argument("--bytes", type=int, required=True)
    ap.add_argument("--pages", type=int, required=True)
    ap.add_argument("--available-mib", type=int, required=True)
    ap.add_argument("--reserved-mib", type=int, default=0)
    ap.add_argument("--margin-mib", type=int, default=1024)
    a = ap.parse_args()
    print(json.dumps(choose(a.bytes, a.pages, a.available_mib * MiB, a.reserved_mib * MiB, a.margin_mib * MiB,
                            coeffs=load()), indent=2))
