# Task-level competitive baseline — issue #6

**Desk-research snapshot:** 2026-11-15. **Method:** publicly available vendor/project documentation and published project notes; no independent hands-on execution of Cellebrite Reader, Guardian, IPED, or UFEDLib_py. This is a hypothesis filter, not a feature certification, market survey, or legal/licensing opinion.

## What already works elsewhere

| Task professionals may need | Documented current workflow / tool | Friction worth *testing*, not observed in a customer | Evidence and uncertainty | Opportunity hypothesis |
| --- | --- | --- | --- | --- |
| Review a received UFDR for messages, contacts, events, or dates | **Cellebrite Reader:** opens UFDR reports, searches/filters artifacts, timeline view, bookmarks/tags, focused report export. **IPED:** handles UFDR, full-text search, timeline and bookmarks/reports. | Perhaps none. A second UFDR viewer would replicate existing functionality. | Vendor and project documentation [1][2][4]. No test with customer files or recent versions. | **Do not pursue another viewer** without a specific failed task. |
| Hand a subset of reviewed material to lawyers or an expert | Reader: tagged selections and focused reports. IPED: indexed reports built from bookmarked items. Guardian: review/sharing and other package-specific features. | Do reviewers still manually write *what the records do not establish*, questions requiring specialist verification, and source/reference crosswalks outside these tools? | Report export confirmed [1][3][5]; difficulty of handoff **unverified**. | **Candidate gap A:** a modest, inspectable *review-to-expert handoff* output rather than a new extraction engine. |
| Review messages alongside PDFs, phone records, or other source families | IPED: multi-case processing, various disk/document inputs, UFDR support, search, metadata, timeline, graph analysis. Cellebrite **Guardian Investigate** markets multi-source UFDR/CDR/document analysis and cross-case investigation. | A legal team might receive a patchwork of reports rather than originals, and lack time, access, or budget for a full processing environment. | Capabilities confirmed in descriptions [2][4]; procurement/actual access, version and data import constraints not tested. | **Candidate gap B:** explainable *cross-export reconciliation* for a narrowly defined delivered-material workflow. Do not claim incumbent tools lack this. |
| Trace an observation to the original information | Reader and IPED expose records and report artifacts; IPED provides viewers and metadata. Their documentation does not establish that every report reference maps to an original physical device byte offset. | Recipients may need an explicit distinction between the *exported report record* and the *underlying device artifact*, plus documented uncertainty. | General capabilities documented [1][2][3]; specific lineage behavior is **untested**, not absent. Existing VIII [synthetic contract](../research/ufdr-message-structure.md) limits claims about `source_index`. | Test whether a *bounded provenance/limitations section* saves review/rework time. |
| Review without vendor-hosted cloud processing | Reader is a free portable UFDR viewer distributed through the Cellebrite portal. IPED supports local processing and portable cases. Guardian is a cloud platform. | Is client-managed deployment actually a purchasing requirement, or do existing local tools already suffice? | Official product/project docs [1][2][4]. Security guarantees, offline behavior and license suitability are **not tested**. | **Candidate constraint C**, not standalone differentiation. |

### Direct alternatives and constraints

- **Cellebrite Reader**: official current page advertises free UFDR review, search/filter, timeline, bookmarking and report generation. Access to the latest download requires the Cellebrite Community Portal. We have **not** installed it, tested report links, or assessed supported versions. [1]
- **IPED**: established open-source forensic processing/indexing platform. README explicitly lists UFDR, full-text search, OCR, graph/timeline analysis, tags and HTML/CSV/portable-case reports; the user manual describes indexed reports. Using IPED as a baseline is essential, not optional. The repository's `LICENSE.txt` is GPL-3.0-or-later with additional permissions; review derivative/distribution obligations before contemplating integration. [2][3][6]
- **Cellebrite Guardian**: avoid comparing VIII only against Reader. Guardian's current product page advertises multi-source analysis, case sharing, access logs and investigatory workspaces, with different capabilities by package. It is cloud-hosted. We have not confirmed local customer access, contractual pricing or every package entitlement. [4]
- **UFEDLib_py**: PyPI describes a Python CLI/API for `.ufdr` and `report.xml`, model discovery, one-pass `parse_all`, and structured report data. That is enough to argue **against implementing a parser from scratch as a discovery experiment**. We did **not** install it, verify that it handles the VIII fixture or real export variations, or verify its repository license/integration terms. If technical adoption is proposed later, perform a separate license, format coverage, error handling, and provenance assessment. [7]
- **UFEDLib (C#)**: upstream documents UFDR-to-object parsing and an MIT license, providing a second reuse candidate. The project states that exporting into general-purpose formats is outside its core library. [8]
- **Real-world compatibility nuance:** an IPED discussion from February 2026 describes a particular UFDR/Physical Analyzer report variant that IPED could not index until re-exported; this is a *reported case with discussion*, not proof that IPED routinely fails on UFDRs, nor a VIII product opportunity. [9]

## Narrow candidate tests (ranked as interview questions, not a product roadmap)

**A. Preliminary review-to-expert handoff.** Ask an attorney to walk through the last time a question from a digital-evidence report had to be sent to a forensic specialist. How did they preserve the exact reference, underlying ambiguity, prior analysis, and desired follow-up? Who prepared the request, who checked it, and what time or cost was lost? Compare their real artifact against the [synthetic package](synthetic-review-package.md). **Falsification:** Reader/IPED/Guardian or a simple existing document template already handles this quickly and acceptably.

**B. Mixed-source explanation for a specific matter.** Ask for a remembered case where a PDF report, message export and another record were compared. What was the exact reconciliation step, especially timezones and source identifiers? Was this repeated across matters? **Falsification:** a working IPED/Guardian workflow or existing report format already makes the step reliable, with no relevant buyer pain.

**C. Data location is a constraint.** Ask which data handling and execution arrangements were actually *required* for the last engagement, not which architecture sounds attractive. **Falsification:** a portable local tool already satisfies the need, or the person has no purchasing authority.

**First interview priority:** A. It is demonstrable with a single synthetic source and does not demand new code. If B or C emerges organically as a stronger recurring problem, change course.

## Verification tasks before selecting any feature

1. Ask practitioners to **show their real workflow without disclosing case contents** (redacted templates or screen walkthrough using synthetic data only); identify exactly what Reader/IPED/Guardian can already deliver.
2. For each alleged limitation, record tool/version, license or package tier, input/export type, exact reproduction step, and whether the gap is a skill/configuration issue. Mark unsupported claims as **unknown**, not **no**.
3. If UFDR becomes necessary, separately run candidate parsers against our fixture and suitably licensed integration data; check missing fields, stable source reference semantics, protected containers, version differences and library license.
4. Do not build ingestion, AI, OCR, desktop UI, or a general feature matrix as part of #6.

## Sources

1. [Cellebrite Reader — official product page](https://cellebrite.com/en/products/cellebrite-inseyets/reader/) and [Reader training](https://cellebrite.com/training/cellebrite-reader/)
2. [IPED — official project README](https://github.com/sepinf-inc/IPED)
3. [IPED user manual — analysis and report creation](https://github-wiki-see.page/m/sepinf-inc/IPED/wiki/User-Manual) (community-rendered mirror; verify against upstream wiki before implementation)
4. [Cellebrite Guardian — official product and package descriptions](https://cellebrite.com/en/products/guardian/)
5. [Cellebrite Reader — vendor explanation of report creation](https://cellebrite.com/en/ask-the-expert/cellebrite-reader-part-3-driving-your-investigation-with-reader/)
6. [IPED LICENSE.txt — GPL-3.0-or-later and exceptions](https://github.com/sepinf-inc/IPED/blob/master/LICENSE.txt)
7. [UFEDLib_py on PyPI — published CLI/API behavior](https://pypi.org/project/ufedlib-py/)
8. [UFEDLib (C#) upstream README](https://github.com/SEilers/UFEDLib)
9. [IPED discussion #2801 — particular UFDR variant](https://github.com/sepinf-inc/IPED/discussions/2801)

**Decision at desk-research stage:** existing capabilities invalidate **viewer/parser novelty** as a value proposition. They do *not* establish that A or B is unmet or commercially meaningful. Interviews and an actual buyer next step remain mandatory.
