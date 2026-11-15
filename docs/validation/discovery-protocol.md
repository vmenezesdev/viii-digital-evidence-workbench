# Customer discovery and paid-pilot protocol — issue #6

**Status:** research instrument only. No participant has been interviewed for this issue, and no demand, buyer commitment, or authorization is implied.

## Recruiting and consent

Target **three relevant people**: approximately two attorneys/legal decision-makers who commission or consume technical evidence reviews and one professional who performs forensic reviews. If the actual purchasing chain differs, adapt deliberately and document why.

Suggested invitation: “We're researching how legal teams review digital evidence and prepare questions for technical experts. Could we discuss a recent workflow and what existing tools did or did not solve? No case files or confidential details are needed. This is research, not a sales pitch.”

Before taking notes, explain purpose, whether notes may be retained, that participation is voluntary, and that **only authorized anonymized summaries** could appear in the public repository. Do **not** request or record privileged material, personal identifiers, client names, sealed facts, originals, case excerpts, screenshots, or nonpublic exports. Store private consent/contact details separately and securely, not in Git. Ask for permission before recording; default to **no recording**.

## Interview (approx. 25–35 minutes; do not start with the product)

### 1. Recent behavior and concrete task

- “Think of the most recent review involving digital evidence. What was the specific question you needed to answer?”
- “What exactly arrived — UFDR, Cellebrite Reader package, PDF, spreadsheets, call records, images, or a mixture? Who provided it?”
- “Walk me through each step, tool and handoff, from receiving the material to the final deliverable.”
- “Where did you stop to verify, correct, or consult a specialist? What could go wrong if someone skipped that step?”
- “What was the last tool, procedure, or person you tried to resolve it? Why was the outcome satisfactory or not?”
- “How often did a similar task arise in the last three months? How long and how much did it cost (ranges are fine)?”
- “Who did the work, who signed off, and who controlled the budget?”

### 2. Discover the constraint, not an imagined feature

- “What references must you preserve so that another person can verify your conclusions?”
- “Did the relevant records stay in one tool or move across emails, reports or spreadsheets?”
- “What were the actual technical, security or contractual restrictions on where the review happened?”
- “How did you handle uncertainties, conflicting timestamps, or missing context?”
- “What would happen if you simply continued with the current workflow? Is it annoying, costly, risky, or acceptable?”

### 3. Only after the unaided workflow: critique the outcome

Show [the synthetic preliminary review package](synthetic-review-package.md) **after** asking about the last real workflow.

- “Which parts does your current tool already produce?”
- “Would this have changed anything in the last matter? What exactly?”
- “What is missing, misleading, or overconfident?”
- “Could a simple Word/Excel template work as well?”
- “Who would verify this output? What professional responsibilities cannot be delegated?”

Make it explicit that the package is a **manually written illustration**, not output from implemented software.

### 4. Commercial test (only if a genuine unsolved task appears)

- “Who would decide to spend money to address this? Who would approve security and procurement?”
- “What did your team pay previously (software license, staff time, external expert, or other alternative)?”
- “If we scoped just one deliverable for one authorized synthetic/public test, what criteria would make the result useful?”
- “What *range* of budget would you have available, and which purchasing step would be required?”
- “Would it make sense to arrange a follow-up scope discussion with the budget owner on that basis?”

**Do not count compliments, hypothetical price guesses, or agreeing to receive updates as willingness to pay.** A credible signal is an agreed next action with a relevant buyer, a plausible budget or procurement path, and a task worth evaluating.

**Safety gate:** interest in a pilot does not authorize use of confidential or sealed data. Any subsequent pilot involving nonpublic inputs needs separate lawful authorization, data handling agreement, security/privacy assessment, professional review boundaries, and operational safeguards.

## Anonymized notes template (one per conversation)

Copy this block to a **private** workspace first. Publish only consented, non-identifying summaries. Do not include client or case identifiers.

```markdown
### Interview [A/B/C] — [date, broad role only]
Consent to participate / anonymized publication: [record privately; public: authorized yes/no]
Role in workflow: [user / technical reviewer / economic buyer / other]
Recent task (firsthand account): [...]
Formats actually received: [...]
Current tools and steps (firsthand): [...]
Handoffs and required verification: [...]
Repetition and impact (reported quantities, ranges and context): [...]
Alternatives tried and why they did/did not suffice: [...]
Who controls spending; possible budget route: [...]
Feedback after synthetic-package demonstration: [...]
Specific paid-pilot follow-up (if any): [...]
Unverified assertions requiring corroboration: [...]
Researcher interpretation / hypothesis, distinctly labeled: [...]
Follow-up/unknowns: [...]
Publication safety review: [no identifying/confidential details]
```

No response is entered here in advance. “Unknown” is better than an invented fact.

## Cross-interview synthesis (to complete only with real evidence)

| Candidate task | Independent accounts reporting same material problem | Verified competing-tool capability and actual limitation | Frequency and concrete impact | Buyer and budget path | Decision |
| --- | --- | --- | --- | --- | --- |
| Review-to-expert handoff | Not assessed | See [baseline](competitive-baseline.md); no hands-on test | Not assessed | Not assessed | **Open** |
| Mixed-source reconciliation | Not assessed | See [baseline](competitive-baseline.md); no hands-on test | Not assessed | Not assessed | **Open** |
| Customer-controlled execution | Not assessed | Available local alternatives noted; constraints unverified | Not assessed | Not assessed | **Open** |

## Decision gate

Issue #6 can close with a **negative** result, but not just because the documents exist. Record all of:

1. Three interviews complete (or explicitly justify any changed sample).
2. At least two independently report a materially similar problem, **or record that the threshold failed**.
3. Current-tool comparison corrected with concrete field evidence, including no-differentiation findings.
4. Actual format, frequency, consequence, user, buyer, alternatives and budget/purchasing path.
5. At least one credible next step toward a *paid* pilot, **or explicitly report its absence**.
6. A written **proceed / pivot / do-not-prioritize** rationale, including the minimum candidate work product and human/safety boundaries if proceeding.

**Default decision now: insufficient evidence; continue discovery.** Do not create an implementation issue solely from desk research or the illustrative package.
