# VIII: Digital Evidence Workbench

**Language:** English · [Português (Brasil)](README.pt-BR.md) · [Français](README.fr.md)

**An open-source workbench in the making for exploring digital evidence, understanding what it shows, and tracing findings back to their sources.**

Digital evidence is rarely handed over in a convenient form. A single case may involve thousands of pages, exported conversations, phone records, audio files, and reports from different tools. Finding something relevant is only part of the job: you also need to know **where it came from, how it was processed, and what its limitations are**.

VIII (Digital Evidence Workbench) is an early-stage effort to make that work more accessible to legal teams, more reproducible for forensic practitioners, and more extensible for developers.

> [!IMPORTANT]
> **Project status: inception / pre-alpha.** VIII is a project proposal under active design—not a usable forensic product yet. There are no stable releases, validated workflows, or production-ready security guarantees. **Do not upload or process real case material with this project at this stage.**

## What could you do with VIII?

The goal is to help people move from *a collection of files* to *an investigation they can explain and verify*.

Here are a few **illustrative workflows we want to support**, not features available today:

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

The ordering can change as practitioners validate the workflows.

## Where processing happens

The long-term goal is **one workbench with three execution modes**, not three different products:

| Mode | What it means | Important boundary |
| --- | --- | --- |
| **Cloud** | Run authorized jobs on managed compute | Data transfer, access, and cost must be explicit |
| **Hybrid** | Keep selected steps local; offload others | Only specifically approved inputs should leave the device |
| **Local / air-gapped** | Run the full supported workflow on an isolated machine | Models, dependencies, licensing, and updates must work without a network |

**Air-gapped is a security property to verify, not a marketing switch.** It will not be claimed until offline installation and behavior can be tested.

### Technical direction (under evaluation)

The proposed architecture separates forensic logic from *where* a task runs:

```text
Desktop workbench
  ├── Case and evidence workspace
  ├── Findings, search, and provenance
  └── Processing interface
       ├── Local native workers          (no Docker required)
       └── Distributed job execution     (when authorized)
            └── Bacalhau — candidate runtime
                 ├── Portable jobs, where practical
                 └── Containerized workers for heavier tasks
```

[Bacalhau](https://github.com/bacalhau-project/bacalhau) is being evaluated for distributed execution. Rust/Tauri, Go, Python-based processing tools, WASM, and OCR pipelines are **implementation candidates**, not finalized dependencies or shipped integrations.

Keeping workers behind a stable interface should make it possible to run suitable tasks locally without installing Docker, while allowing containers on remote compute nodes where they make sense.

## Help shape VIII

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

**MPL 2.0 is the planned license for the project core.** The licensing files, third-party notices, and contribution guidance will be added before the initial source release. Until then, please do not infer permission to reuse unpublished or unlicensed code.

The project starts under [`vmenezesdev`](https://github.com/vmenezesdev). The intention is to keep the core openly inspectable and usable for both noncommercial and commercial work, while encouraging distributed improvements to covered source files to remain open under the terms of the MPL.

[Luminiware](https://luminiware.com) may provide optional commercial services around the project in the future, such as managed compute, deployment, support, preliminary human review, and access to independent forensic specialists. **These are proposed offerings, not available services announced by this repository.** The open project and commercial contracts will be documented separately.

---

**The first question is not “How much can we automate?” It's “What must practitioners be able to verify?”**

If you work with digital evidence, [tell us which workflow should come first](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues).
