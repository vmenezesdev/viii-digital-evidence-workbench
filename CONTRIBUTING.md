# Contributing to VIII

**Languages:** English · [Português (Brasil)](CONTRIBUTING.pt-BR.md)

VIII is a pre-alpha open-source project for exploring digital evidence with traceable, reviewable results. Lawyers, forensic practitioners, researchers, and developers are all welcome. **You do not need to write code or speak English to contribute.**

For project scope and limitations, see the [README](README.md). No forensic workflow or security guarantee is validated for real cases yet.

## Choose how to contribute

- **Describe a forensic workflow.** Tell us what you need to investigate, how you currently approach it, and what you would need to check before trusting a result. No technical solution is required.
- **Raise a technical issue.** Report reproducible behavior, investigate a format or dataset, propose a test, or discuss an engineering trade-off.
- **Improve documentation or translations.** Clarify concepts, identify ambiguous claims, and make instructions accessible.
- **Propose code.** During pre-alpha, discuss significant implementation changes in an issue before opening a pull request. Small, focused documentation and test improvements are welcome.

Use the [issue chooser](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/new/choose) to select **Forensic workflow** or **Technical issue**. A blank issue is also available if neither fits.

## Languages and accessibility

- **Issues and comments are welcome in any language.** English, Brazilian Portuguese, and French are particularly welcome. Do not hold back a domain observation because you cannot write a technical report in English.
- **English is the coordination language for technical work:** code, identifiers, tests, API contracts, pull requests, architecture decisions, and maintainer-authored implementation tasks should ordinarily use English.
- Maintainers should acknowledge and clarify contributions **in the contributor's original language where feasible**, especially at meaningful decision points. We cannot promise immediate responses or translations of every engineering comment.
- A maintainer may add an **English technical summary in the original issue** to make the context available to the development community. The summary is a working interpretation, not a replacement for the original report.
- If a report creates a clearly separable implementation task, a maintainer may open an **English technical issue linked in both directions**. Do not create translated duplicate issues by default. Keep decisions and important progress visible in the original issue.
- Automated translation or drafting may assist maintainers, but **humans remain responsible** for checking meaning, sensitive terminology, and technical accuracy. Translation assistance is not currently an automated project service.

In particular, legal and forensic terms (for example, *evidence*, *artifact*, *provenance*, and *chain of custody*) can have context-dependent meanings. Ask before silently converting a domain statement into an engineering requirement.

## What makes a useful issue?

For a **forensic workflow**, describe the question you are trying to answer, the kind of material involved (for example, a UFDR export), the current difficulty, and what you would need to verify a result. Everyday language is fine. You do not need to provide a fix, a mockup, or a file.

For a **technical issue**, state the problem or research question, the expected outcome, and—when available—reproduction steps, synthetic examples, constraints, and proposed validation. Separate confirmed facts from assumptions. A research question is a valid contribution even without a solution.

A maintainer may ask follow-up questions, add a concise English summary, suggest a narrower scope, or link a separate implementation issue. We will not treat a translated summary as proof that a domain expert has endorsed the interpretation.

## Protect people and case material

**Never publish real case evidence or sensitive information in public issues, pull requests, fixtures, or screenshots.** This includes sealed documents, client records, privileged communications, identifying personal information, credentials, and nonpublic forensic exports.

Use wholly synthetic examples, or explicitly public material that you are authorized to share and whose distribution terms you have checked. A public download link is **not** by itself a license to redistribute a dataset. Prefer an upstream link, source attribution, checksums, and reproduction instructions over copying external corpora into this repository.

**Do not post exploit details or security vulnerabilities in public issues.** A private security-reporting process has not yet been documented; please do not share sensitive vulnerability details through public GitHub discussions while that process is being established.

VIII is not ready for real-case processing. Contributions should not imply that proposed functionality has been implemented, validated, or certified.

## Pull requests

- Link a relevant issue for substantial code or architecture changes, and keep the change narrow enough to review.
- Describe what changed, why it matters, and how you checked it. Document any unresolved limitations or assumptions.
- Prefer reproducible tests using synthetic data, with source-to-output traceability where applicable.
- Follow the project's existing language convention for technical artifacts (English). If you need help preparing a technical description, begin with an issue in your preferred language.
- Avoid introducing a dependency, a licensing assumption, or a claim of forensic correctness without justification.

Maintainers review technical decisions in English when practical and can summarize important outcomes back to the original contributor in their language. This is a lightweight, revisable workflow for the project's inception stage—not a promise of guaranteed support, translation, or turnaround times.
