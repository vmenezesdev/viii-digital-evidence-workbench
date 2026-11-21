# VIII: Digital Evidence Workbench

**Language:** English · [Português (Brasil)](README.pt-BR.md) · [Français](README.fr.md)

**An open-source workbench in the making for exploring digital evidence, understanding what it shows, and tracing findings back to their sources.**

Digital evidence is rarely handed over in a convenient form. A single case may involve thousands of pages, exported conversations, phone records, audio files, and reports from different tools. Finding something relevant is only part of the job: you also need to know **where it came from, how it was processed, and what its limitations are**.

VIII (Digital Evidence Workbench) is an early-stage effort to make that work more accessible to legal teams, more reproducible for forensic practitioners, and more extensible for developers.

> [!IMPORTANT]
> **Project status: pre-alpha.** VIII is under active design, with two experimental slices that run on synthetic data. It is not a usable forensic product yet: there are no stable releases, validated forensic workflows, or production-ready security guarantees. **Do not upload or process real case material with this project at this stage.**

## What exists today

Two experimental slices, tested on one Windows workstation with **synthetic data only**:

- **Large PDFs (experimental).** Import a PDF of up to 30,000 pages, search pages as soon as they are processed, OCR scanned pages in Portuguese, and ask a local model (llama.cpp) whose citations open the original's physical page. Jobs run through Bacalhau with WASI workers by default, so Docker is not required; Docker is an optional accelerator. Large PDFs are split into verified 40-page chunks, and every result points back to the immutable original. On a synthetic 30,000-page, 1.25 GB file with 3,001 scanned pages, the complete workflow took 6 h 04 min with WASI and 2 h 30 min with Docker, with no failed pages. Start with the [PDF documentation](docs/poc/pdf/README.md).
- **UFDR messages (proof of concept).** A Docker-free WASI worker normalizes a synthetic UFDR message fixture with source references, and a development desktop slice displays the result. See the [validation report](docs/poc/issue-9-validation.md) and [issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9).

Neither slice has been evaluated on real evidence, installed on a clean machine by end users, or reviewed by forensic practitioners.

## What could you do with VIII?

The goal is to help people move from *a collection of files* to *an investigation they can explain and verify*.

Here are a few **illustrative workflows we want to support**. Only the scanned-documents workflow exists, as the experimental PDF slice above:

| Starting point | Questions to investigate | Intended output |
| --- | --- | --- |
| A UFDR XML export | Which conversations, contacts, or events deserve a closer look? | Searchable records and source-linked findings |
| A set of scanned case documents | Where is a person, date, or event mentioned? | Extracted text with links to the original pages |
| Audio recordings and call records | What was said, when, and in which recording? | Searchable transcripts with time references and uncertainty noted |
| Evidence from multiple sources | Do the timestamps and accounts align? | A traceable chronology of relevant events |

**Automation should assist investigation, not make evidentiary claims on its own.** Results need to remain reviewable by people with the appropriate expertise.

## Who is this for?

**For forensic practitioners:** Help shape workflows that preserve context, expose assumptions, and make processing reproducible. Your knowledge of where tools fail is as useful as code.

**For lawyers and legal teams:** Help define practical questions, useful reports, and understandable explanations of technical limitations. You do not need to be a programmer to participate.

**For developers and researchers:** Help build parsers, processing workers, tests, interoperability, and the local-first infrastructure behind the workbench.

## Design principles

- **Evidence first.** Preserve original material; treat extracted text, transcripts, indexes, and AI outputs as derived artifacts.
- **Traceable by default.** Findings should point back to their source, with enough processing metadata to understand how they were produced.
- **Local control.** The user should be able to choose where processing happens and which data, if any, leaves their environment.
- **Humans remain responsible.** A useful tool should expose uncertainty and support expert review—not replace qualified forensic or legal judgment.
- **Open to scrutiny.** Prefer documented formats, reproducible tests, and workflows that specialists can inspect and challenge.
- **Extensible without unnecessary setup.** A lawyer should not need to understand Docker or distributed systems to use the eventual desktop application.

## Where the project starts

The first planned slice is deliberately narrow: **a local workflow for structured digital evidence, beginning with UFDR XML**.

Why start there? It offers a concrete format and useful investigative questions without requiring OCR, speech recognition, or GPU infrastructure on day one.

Initial milestones (proposed):

1. Define a model for evidence, derived artifacts, provenance, and processing history.
2. Parse a small, synthetic UFDR XML fixture locally and generate inspectable, source-linked output.
3. Establish repeatable tests and clear handling of unsupported or ambiguous records.
4. Explore a desktop interface and installation that does not require Docker.
5. Evaluate optional remote execution and heavier document/audio pipelines.

The UFDR proof of concept covers early versions of milestones 1–3 and a development desktop slice; the PDF slice added OCR earlier than planned and was validated without Docker. Installation on a clean machine, updates, cross-host execution and practitioner validation remain open ([issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9), [issue #11](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/11)). The ordering can change as practitioners validate the workflows.

## Where processing happens

The long-term goal is **one workbench with three execution modes**, not three different products:

| Mode | What it means | Important boundary |
| --- | --- | --- |
| **Cloud** | Run authorized jobs on managed compute | Data transfer, access, and cost must be explicit |
| **Hybrid** | Keep selected steps local; offload others | Only specifically approved inputs should leave the device |
| **Local / air-gapped** | Run the full supported workflow on an isolated machine | Models, dependencies, licensing, and updates must work without a network |

**Air-gapped is a security property to verify, not a marketing switch.** It will not be claimed until offline installation and behavior can be tested.

### Technical direction

The architecture separates forensic logic from *where* a task runs. Its current decisions are recorded as [architecture decision records](docs/adr):

```text
Desktop workbench                       Go + Wails v3 (ADR-0002, proposed)
  ├── Case and evidence workspace       immutable originals, SHA-256 identity
  ├── Findings, search and provenance   local index; every result links to its source
  └── Processing                        Bacalhau as the single orchestrator (ADR-0001, accepted)
       ├── WASI workers                 portable default, no Docker required
       └── Docker workers               optional, for performance or native dependencies
```

- **One job lifecycle, local or remote.** [Bacalhau](https://github.com/bacalhau-project/bacalhau) runs every processing job, on the workstation today and on authorized remote nodes later. Direct native jobs outside Bacalhau are excluded from the product; the PDF slice follows this, while the UFDR development slice still offers native execution as its default and has yet to be aligned. Cross-host execution has only been exercised on a single physical machine so far.
- **Large documents are processed in verified pieces.** PDFs are chunked by a WASI worker before OCR, and the chunks are operational copies bound to the original, never evidence themselves ([ADR-0003](docs/adr/0003-chunk-large-pdfs-with-flpdf-wasi.md)).
- **Inference stays local and optional.** Questions use a separately installed local model; search and OCR work without it.

Air-gapped operation is still a goal: restricted-network first use passed on the development workstation, but offline installation and updates have not been verified.

## Help shape VIII

Contributions are welcome in any language. Read [Contributing](CONTRIBUTING.md) or [Como contribuir](CONTRIBUTING.pt-BR.md), then use the [issue chooser](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/new/choose). Technical coordination is generally in English; maintainers aim to keep the original contributor informed in their language when feasible.

**You do not need to write code.** At this stage, a clear description of a difficult workflow can be more valuable than a large pull request.

Here are useful starting points:

| If you are... | A valuable first contribution |
| --- | --- |
| A forensic practitioner | Describe an export format, interpretation pitfall, or validation step that current tools handle poorly |
| A lawyer | Describe a recurring question about digital evidence and what you would need to verify the answer |
| A developer | Propose a small parser test, synthetic fixture, worker interface, or reproducibility check |
| A researcher or educator | Suggest a public or synthetic benchmark, methodology, or documentation improvement |

**Start a conversation:** [open an issue](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues) with the problem, the type of material involved, what a correct result would look like, and why existing tools fall short. Proposals and corrections are welcome; substantial implementation PRs should be discussed first while the architecture is still forming.

> [!CAUTION]
> **Do not post real evidence, sealed case documents, client data, privileged communications, credentials, or identifying personal information** in public issues or pull requests. Use synthetic examples or material you are authorized to publish. Please do not report exploitable security details publicly; private reporting instructions will be established before releases.

## What VIII is *not*

VIII is **not** a forensic certification, an official forensic examination, a legal opinion, or a substitute for qualified expert review. The software cannot by itself establish the authenticity, admissibility, or evidentiary weight of material. Those questions depend on methodology, context, applicable law, and professional judgment.

## License and stewardship

**The project core is licensed under MPL 2.0** ([LICENSE](LICENSE)). Third-party notices for the tools and models used by the workers (for example Bacalhau, Tesseract and its Portuguese model, and flpdf) still need to be compiled before any release.

The project starts under [`vmenezesdev`](https://github.com/vmenezesdev). The intention is to keep the core openly inspectable and usable for both noncommercial and commercial work, while encouraging distributed improvements to covered source files to remain open under the terms of the MPL.

[Luminiware](https://luminiware.com) may provide optional commercial services around the project in the future, such as managed compute, deployment, support, preliminary human review, and access to independent forensic specialists. **These are proposed offerings, not available services announced by this repository.** The open project and commercial contracts will be documented separately.

---

**The first question is not “How much can we automate?” It's “What must practitioners be able to verify?”**

If you work with digital evidence, [tell us which workflow should come first](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues).
