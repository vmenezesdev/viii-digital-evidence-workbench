# Run a PDF experiment without an AI session

Use Windows PowerShell from the repository root. The CLI starts a hidden,
independent PowerShell process running the existing benchmark collector and
packaged VIII application. Closing the invoking terminal or the AI session does
not stop the experiment. No model or AI service is needed to control it.
The launcher uses [local asynchronous process creation](https://learn.microsoft.com/en-us/powershell/module/microsoft.powershell.management/start-process?view=powershell-5.1)
with ShellExecute. Log redirection happens inside the runner so it does not keep
the caller's redirected pipes open.

## Control the current 30,000-page experiment

The existing profile has been adopted without replacing its PDF or accepted
pages. Its package and inputs are saved in `experiment.json`:

```powershell
$run = '.poc-runtime/pdf/benchmark-wasi-simd-30000-02'
./scripts/pdf-experiment.ps1 status -Results $run
./scripts/pdf-experiment.ps1 pause -Results $run
./scripts/pdf-experiment.ps1 resume -Results $run
./scripts/pdf-experiment.ps1 logs -Results $run -Tail 3
./scripts/pdf-experiment.ps1 results -Results $run -Output "$run/report.json"
```

`start` and `resume` return immediately with the detached collector PID and log
path. `status` reports whether the app and collector are alive, the last control
state, searchable/OCR/pending/failed counts, current range and attempt, sampled
resource peaks, and any final summary. `live: false` means the progress is a saved
snapshot; its `at` timestamp identifies when it was observed. Results remain
inspectable while paused and after processes exit.

`pause` stops only processes whose executable path and creation time match the
profile's ownership records. It stops submission, requests cancellation of the
recorded job, waits for terminal job **and execution** states, saves the actual
description, then closes the owned runtime. It preserves originals, page records,
index and job history. An incomplete cancellation remains visible and must be
resolved by repeating `pause` before `resume`.

`resume` verifies input identity, reopens the same profile, reconciles the previous
attempt and continues missing work. A repeated resume while the collector is
alive returns `already-running`. Locks prevent two CLI collectors for one profile.
Recorded pause intervals are added once to the existing `user_pause_ms` accounting;
raw wall time and earlier peaks are preserved.

`results` returns the validated final summary when available, otherwise a clearly
partial snapshot. `-Output` writes the same JSON to disk. It does not publish to
GitHub. The collector continuously appends measurements to `events.jsonl`, saves
`state.json` and `progress.json`, and writes `summary.json` after the original/hash
and late-page search assertions pass. The runner then closes its owned app and
runtime. Commands and lifecycle outcomes are retained in `commands.jsonl`;
`runner-*.stdout.log`, `runner-*.stderr.log` and `runner-status.json` expose failures.

The former ignored `finish-benchmarks.ps1` queue was retired during adoption.
This CLI controls one experiment. Docker and concurrency measurements remain
explicit follow-up commands from [the product report](pdf-product.md).

## Start a fresh experiment

Build the runtime bundle and generate the synthetic fixture using the existing
[reproduction instructions](pdf-product.md), then choose a fresh results directory:

```powershell
./scripts/pdf-experiment.ps1 start -Package bin/pdf-mvp `
  -PDF .poc-runtime/pdf/fixtures/mixed-30000.pdf `
  -Results .poc-runtime/pdf/benchmark-cli -Port 49181
```

Use `-Engine docker` on `start` for a separate Docker benchmark after preparing
the images. Each experiment needs its own data directory and an available loopback
port. To adopt another existing collector, run `resume -Results <directory>
-Package <its-existing-bundle>` once; subsequent commands use the saved config.

## Failure handling and limits

The CLI does not install a Windows service or configure automatic startup after
reboot. Windows shutdown, sign-out or sleep can interrupt/suspend the work. Use
`status`, `logs` and explicit `resume` after returning. A failed collector records
the error and leaves the owned app/runtime available for inspection; processing
may still be active, and `pause` remains available. Resume reuses the original
clock and records the telemetry gap. There is no blind background resubmission
loop or automatic default-concurrency change.

Moving the source, bundle or results directory requires updating their saved
paths; the source PDF must retain its identity. Keep the profile on local storage
and use this controller for lifecycle operations. Simultaneous manual application
instances on the same data directory remain outside the experiment's scope.

## Repeat the lifecycle check

```powershell
./scripts/verify-pdf-experiment.ps1 -Package bin/pdf-mvp
```

The check invokes each command in a separate shell, observes a real running OCR
job on the small synthetic fixture, pauses and confirms terminal executions,
exports a partial checkpoint, resumes, checks duplicate-resume/pause accounting
and accepted page hashes, and waits for a validated final summary and shutdown.
It uses a fresh results path containing a space, exercising detached argument
quoting as well. Its `cli-test.json` records the result.

Windows validation on 18 November passed the [12-page lifecycle](results/pdf-mvp/cli-lifecycle-01/cli-test.json)
with eight accepted page hashes preserved, and the [final two-page check](results/pdf-mvp/cli-lifecycle-final/cli-test.json)
with a real OCR job, partial export, exactly-once pause accounting, duplicate
resume and automatic process cleanup. These are functional checks, not OCR
throughput comparisons. A [stale-status regression](results/pdf-mvp/cli-stale-status-check.json)
also resumed the large profile despite a deliberately simulated old failure
record; the calling command returned in about 1.45 seconds. No OCR failure was
induced in that check. Live CLI lifecycle validation covers WASI; Docker lifecycle
through this controller has not received a separate live run.
