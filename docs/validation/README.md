# Validation kit for issue #6

**Status (2026-11-15): discovery in progress, NOT validated.**

This directory contains the desk-research and interview materials for [issue #6](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/6). Its purpose is to choose an evidence-review *task* worth validating before selecting an input format or building software.

| Artifact | Purpose | Status |
| --- | --- | --- |
| [Task-based competitive baseline](competitive-baseline.md) | What existing tools demonstrably do, potential handoff friction, and what still needs testing | Desk research only |
| [Synthetic review package](synthetic-review-package.md) | Show a human-reviewable **output**, not a new parser or application | Hand-authored illustration |
| [Customer discovery protocol](discovery-protocol.md) | Interview guide, privacy-safe notes, paid-pilot test, and decision thresholds | Ready to use; **zero interviews claimed** |

## Working hypotheses (not findings)

- **H1 — Review-to-expert handoff:** lawyers may need a lightweight, reviewable list of observations, source references, limitations, and questions for an independent expert.
- **H2 — Mixed-source reconciliation:** teams may have trouble explaining how a finding joins records from multiple export/report formats.
- **H3 — Deployment control:** a buyer may have a meaningful requirement to keep evidence processing under their control.

All three may already be adequately addressed by incumbent workflows. **No product gap or willingness to pay has been established.** A polished synthetic report or an interviewee's compliments are not evidence of demand.

## Evidence still required

- [ ] Three consented, anonymizable conversations about **recent real tasks** (target: two buyers/users from legal teams and one forensic practitioner).
- [ ] At least two independently report a materially similar, consequential unresolved task.
- [ ] Verify the actual tools, output formats, workflow steps, alternative approaches, spending, and task frequency.
- [ ] Test the synthetic package *after* exploring each person's existing workflow; collect critical feedback.
- [ ] At least one credible buyer agrees to a concrete follow-up about a **paid** narrowly scoped pilot and provides a plausible budget/purchasing route.
- [ ] Write a reasoned proceed / pivot / do-not-prioritize decision in [issue #6](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/6); create an implementation issue **only if** the evidence justifies one.

Do not post identifying interview notes, privileged material, sealed records, real evidence, or private commercial terms here. This repository is public.

**No feature, parser, UI, production security safeguard, or forensic determination is implemented in these documents.**
