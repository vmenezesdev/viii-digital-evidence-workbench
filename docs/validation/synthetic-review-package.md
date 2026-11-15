# Synthetic preliminary evidence-review package (illustration only)

**Purpose:** Show the **work product** for hypothesis A in [issue #6](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/6), not demonstrate implemented VIII functionality. **Every observation and question below was authored manually.** There is no ingestion, parsing, extraction, verification, cross-source matching, or automated analysis here.

**Source used:** the wholly fictional checked-in [UFDR XML fixture](../research/fixtures/ufdr/messages-minimal.xml) and its [expected projection](../research/fixtures/ufdr/messages-minimal.expected.json), prepared in #3. No real matter, person, device, or confidential material.

## Example deliverable — preliminary review handoff

**Reference:** SYNTH-REVIEW-001 · **status:** illustrative; not expert-verified  
**Scope:** one synthetic chat, one synthetic instant message; no surrounding device context  
**Source type:** synthetic `report.xml` structure, *not* an authentic export  
**Evidence preservation:** no source acquired or preserved from a device  
**Reviewer:** example only; no qualified professional has reviewed this package  
**Outcome:** request for human verification, **not** a conclusion about the occurrence of any meeting

### Observation 1 — message content is visible in a synthetic report record

| Field | Value |
| --- | --- |
| Observation ID | `SYNTH-OBS-001` |
| Report-level anchor | `model type="Chat" id="synthetic-chat-0001"` → `multiModelField name="Messages"` → `model type="InstantMessage" id="synthetic-message-0001"` |
| Report attribute | `source_index="13"` (reference within the report; **not** a verified physical-device location) |
| Message body | “Meet at the library at noon.” |
| Displayed timestamp | `2025-01-15T12:00:00+00:00` (the fixture's `formattedTimestamp`; no independent timezone validation) |
| Sender shown | `synthetic-party-0001` / “Alex Example” (exported report record, **not** independently attributed to a real person) |
| Recipient | **Unknown**: no `To` field is supplied |
| Source location | `docs/research/fixtures/ufdr/messages-minimal.xml` → chat ID above → message ID above; project contains only this one XML record |
| Confidence/limitation | It is certain that *the supplied synthetic XML* contains these literal values. This does **not** establish a real-world communication, sender identity, timestamp accuracy, acquisition completeness, or authenticity. |

### Open questions for a qualified reviewer

1. Does the supplied export's report-record reference identify the same message in the original tool? Can the original acquisition, version, device and chain of custody be established?
2. Is the displayed timestamp normalized as assumed? What are the original time representation, device timezone and extraction-tool assumptions?
3. Which recipients and adjacent messages, if any, are present in the broader source? Absence of a `To` field is **not** evidence of absence of a recipient.
4. Can the message be independently corroborated by additional sources, without assuming that two similar descriptions refer to the same event?

### Review decision and limitations

**Not ready for any forensic or legal determination.** The only safe next step illustrated here is a human decision to request context and verification. This example is not a legal opinion, expert report, evidence integrity check, or a product output.

## Manual process and reproducibility

1. Open [the fixture XML](../research/fixtures/ufdr/messages-minimal.xml) as plain text.
2. Locate `InstantMessage` with `id="synthetic-message-0001"`; verify `source_index`, `Body`, `TimeStamp/value@formattedTimestamp`, and `From/Party`.
3. Compare those values to [the expected JSON](../research/fixtures/ufdr/messages-minimal.expected.json). The JSON is a *separately authored expected contract*, not an independent source.
4. Verify the fixture checksums using `sha256sum -c SHA256SUMS` from `docs/research/fixtures/ufdr`. This checks the repository fixture bytes only, **not** original-device provenance.
5. Use this document to ask a practitioner which parts are useful, redundant, confusing, or unacceptable compared with their actual handoff. **Do not share it as a forensic finding.**

## One-page critique questions

- What is missing before you could hand this to a colleague or an independent expert?
- Which fields does your existing tool already populate automatically?
- Does the distinction between exported-record reference and verified original-source provenance clarify anything useful?
- What would be the *next person-to-person action* after receiving the document?
- Would an equivalent manually completed template already solve the problem at negligible cost?
- For the last comparable **real** task, how long did preparation, checking and back-and-forth actually take?

**Design deliberately avoids invented cross-source links.** One message in one synthetic report cannot demonstrate genuine reconciliation, chain of custody, or expert review.
