# UFDR message structure: compatibility notes

Status: research notes and a synthetic contract fixture. This is not parser support or forensic validation.

## Authentic structure inspected

The structure reference is the 2025 bachelor thesis *Extraktion und spatiotemporale Visualisierung von standortbezogenen Informationen in UFDR-Dateien* (Hochschule Mittweida). The author reports acquiring and inspecting UFDR exports from prepared Android and iOS devices using Cellebrite UFED4PC 7.73.0.64 and Physical Analyzer 10.5.0.1027. The thesis explicitly says its generated data used fictional identities and synthetic messages; therefore this is an authentic Cellebrite-generated export structure, not real-world case data. The author inspected the container and `report.xml`. The thesis is openly accessible as a PDF at <https://monami.hs-mittweida.de/frontdoor/deliver/index/docId/16451/file/bachelor.pdf>.

In its examined report, the root is a `project` element in namespace `http://pa.cellebrite.com/report/2.0`; decoded artifacts are under `decodedData`. Messages appear as `model type="Chat"` records. A chat has `multiModelField name="Participants"` with `model type="Party"` children and `multiModelField name="Messages"` with `model type="InstantMessage"` children. The thesis's message example documents `id`, `source_index`, `extractionId`, `deleted_state`, `decoding_confidence`, `field name="Body"`, `field name="TimeStamp"` with a `formattedTimestamp`, a `modelField name="From"`, and possible `To` and attachment fields. `source_index` and the record `id` are useful source references, but the thesis does not establish a universal mapping from either to a physical byte offset or original device record.

The thesis is primary research describing actual Cellebrite-generated exports, but its message example is reproduced as a figure/tree view rather than a downloadable original XML file. We inspected the published report and its message hierarchy, not the raw UFDR bytes. Accordingly, the fixture below follows the documented hierarchy and field names; it is independently authored synthetic XML, not a byte-for-byte transcription or a claim that every UFDR version uses this exact shape. Message-to-chat association comes from nesting in the `Messages` multi-model field.

## Public authentic candidate

Joshua Hickman's Android 12 dataset announcement identifies a Cellebrite UFED extraction and a Cellebrite Reader export. It says the Reader download is approximately 8 GB compressed, links the migrated dataset at Digital Corpora, publishes hashes in the dataset documentation, and invites use for training, education, testing, or research. Source: <https://thebinaryhick.blog/2021/12/17/android-12-image-now-available/>. DFIRScience independently documents using the Reader UFDR from that dataset and reports a ZIP-compatible container with root `report.xml`, categorized files, and paths represented in the XML: <https://dfir.science/2022/02/How-to-extract-files-from-Cellebrite-Reader-UFDR-for-ALEAPPiLEAPP>.

This candidate is publicly accessible and has an explicit research/training/testing permission statement, but it contains real device data. This repository does not download, include, or redistribute it. Its announced size makes it unsuitable for the default development/CI workflow. The original announcement links to the migrated Digital Corpora directory and says hashes are in the dataset documentation; we did not independently verify the current archive listing or hash because the package is approximately 8 GB compressed. Before optional integration testing, contributors should check the current Digital Corpora record and its exact usage terms and verify the archive against the published documentation hash. The acquisition provenance is an author's lab dataset; this is not a claim about independent forensic validation.

## Synthetic fixture contract

Files in this directory:

- `docs/research/fixtures/ufdr/messages-minimal.xml` — one fictional chat, two fictional parties, and one fictional message.
- `docs/research/fixtures/ufdr/messages-minimal.expected.json` — deterministic projection of the message fields selected for the initial contract.
- `docs/research/fixtures/ufdr/SHA256SUMS` — SHA-256 of both files.

Expected projection rules: preserve the message `id`, `source_index`, nested chat `id`, message body, normalized timestamp string as written in `formattedTimestamp`, and sender party `id`; preserve each participant's `id` and display name. Do not infer a recipient when the message has no `To` field. Do not treat `source_index` as a byte offset. The fixture intentionally omits `To`, attachment, timezone ambiguity, deletion, and confidence edge cases; those need separate fixtures if supported later.

## Reproduction

1. Read the cited thesis's sections 4.4.1–4.4.4 and figures 4.4–4.9 for the container, root XML hierarchy, Chat, and InstantMessage structures.
2. Use the checked-in synthetic XML only; no corpus download is needed.
3. From `docs/research/fixtures/ufdr`, verify fixture integrity with `sha256sum -c SHA256SUMS` (or use an equivalent SHA-256 implementation on platforms without `sha256sum`).
4. Compare the XML to the expected JSON by the projection rules above. This is a manual contract check until a parser is implemented.

## Limitations and open questions

- Only one documented Cellebrite report family and one message shape are represented. Export versions and platforms can change names, nesting, namespace use, cardinality, and timestamp encoding.
- The public Android 12 corpus has not been downloaded or independently inspected in this work; the direct authentic message hierarchy evidence comes from the academic study's Cellebrite-generated examples using synthetic device data.
- The meaning and stability of `source_index` across exports is not established. `jumptargets` may refer to other report models or files and needs separate observation.
- A message's `id` identifies the report model in the cited example; uniqueness scope and stability across re-exports are not established.
- Missing `To`, body, timestamp, or sender fields must remain explicitly missing; this fixture does not define fallback behavior.
- No claim is made about authenticity, completeness, admissibility, or evidentiary correctness of future VIII results.

