# Differentiation hypothesis: evidence-reviewability gap assessment

**Status:** 2026-11-15 desk-research hypothesis. **Not validated with customers.** This is not legal advice or a claim that any evidence is inadmissible.

## Executive decision

**Do not compete on "AI for UFDR analysis", search, timelines, chain-of-custody checklists, generalized source-linked reports, or "triage before an expert".** Those capabilities and offers are already marketed by established tools and specialized vendors.

**Best narrow candidate worth testing:** **a reviewability gap assessment at the point a defense team receives a forensic report**, before deciding what additional underlying material to request and which questions merit a qualified technical examination.

Customer job: *"Given the reports and files actually made available to me, which technical assertions can I independently trace to supplied material, what is not verifiable with this production, and what should I ask for before investing in a full expert review?"*

The essential scope distinction is **"not verifiable from the supplied set"** versus **"missing from the original evidence"**, and **"source reference found"** versus **"original-device provenance independently verified"**. Neither equivalence is warranted.

## Evidence supporting a plausible job-to-be-done

- **Brazil, concrete legal context:** STJ **AgRg no AgRg no HC 1.041.340/RS**, Fifth Panel, decided **15 Apr 2026**, concerned access to *full forensic copies (UFD/UFDX in addition to UFDR)* and technical documentation in a specific criminal case to permit assisted defense examination. This is not a universal rule for every case or format. [1]
- **Brazil, authenticity requires more than a rhetorical defect:** STJ **AgRg no HC 1.014.212/ES**, decided **10 Feb 2026**, ordered complementary examination in a case involving reasonable doubt about digital evidence integrity; other 2026 decisions decline generic chain-of-custody objections without evidence of a concrete defect or prejudice. Distinguish jurisdictions, facts and context. [2][3]
- **Forensic methodology risk is real:** NIST IR 8354 discusses inaccurate artifact existence, associations, missing/corrupt data and misinterpretation by forensic tools. It does not show a buyer demand for this product. [4]
- **Strong limitation of automated outputs:** STJ **HC 1.059.475/SP**, decided **7 Apr 2026**, rejected evidentiary use of a particular investigator's generative-AI "technical report" attempting to override actual forensic phonetic analysis. The holding **does not ban every AI-assisted internal workflow**; it makes clear that VIII should not present its preliminary synthetic/AI output as expert evidence or a judicial conclusion. [5]

## Collision test: the proposed function is NOT original

| Alternative | Verified vendor/project claim | What it rules out as "our differentiator" |
| --- | --- | --- |
| Cellebrite Reader / Guardian Investigate | UFDR search, bookmark/report; Guardian offers multi-source AI investigations, court narratives, team collaboration, cloud management and defense access. [6] | Another UFDR reader, cross-source AI chat, tag/share/export. |
| IPED | Open-source UFDR processing, deep search, timeline, tagging, reports, extensibility. [7] | Novelty in indexing, forensic parsing, or local-first processing **alone**. |
| Oxygen Forensic Detective / Review Center | Multi-source timeline, correlation, reporting, forensic search. [8] | Generic chronology and correlation. |
| DefendaMe | Brazilian product advertises UFDR, ERB, intercepted calls, PDF review, AI-generated analysis and reported inconsistencies. These are **vendor claims**, not verified performance. [9] | "Brazilian criminal-defense AI for mixed evidence" alone. |
| Alarion | Specifically targets criminal-defense UFDR review, cites sources, suggests gaps/contradictions and expert triage. [10] | "AI source-linked review for defense" or "triage before expert". |
| Cyberlex | Brazil: offers targeted pre-engagement review, chain-of-custody analysis, specialist reports and questions. Advertises **R$ 800** for a 2–3-page *triagem* and audits from **R$ 6,000** (published asking prices, **not transaction data**). [11] | Generic human triage, preparing questions, offering cheaper expert review without an actual cost basis. |
| Criminal Player — Kit de Cadeia de Custódia | Advertises 8 locally/browser-run tools, hash verification, metadata, custody checklist and documents for counsel. [12] | A local hash tool or checklist on its own. |

**Result:** A useful proposed workflow might still be *underserved*, but **differentiation has not been established** by public feature descriptions. The nearest commercial substitutes are Cyberlex (human review), Alarion (AI-assisted defense case review), existing manual Word/Excel templates, and the Criminal Player toolkit—not just Cellebrite Reader.

## Falsifiable product wedge (not a product spec)

**Entry event:** a lawyer receives a police report, expert report, PDF/UFDR export and partial attachments. The issue is **what has actually been made available and linked**, not whether the original evidence must be excluded.

**Suggested output — "reviewability map" (for internal counsel/expert use only):**

1. **Supplied-material manifest** — file name, file SHA-256, format and whether source is supplied / referred to but not supplied / unknown. Hash is only for the *files received*, not proof of collection integrity.
2. **Claim-to-reference ledger** — extract a small number of material technical claims *with verbatim page/paragraph locations* from a supplied synthetic report. For each, link to a precise available exhibit/record if actually present; otherwise mark *reference missing, ambiguous or not independently inspectable*. No "automatic falsehood" labels.
3. **Reviewability matrix** — separately distinguish visible report record, referenced but unavailable underlying source, metadata/logs, extraction method and hash/chain documentation. Missing from package != proof of nonexistence; lack of access != broken chain of custody.
4. **Human decision handoff** — the minimum clarifying questions and specific categories of documents needed for a **qualified** reviewer, with costs/scope to determine later.
5. **Uncertainty/audit trail** — who entered each conclusion, which sources they could read, which checks are mechanically reproducible and which require a professional; no AI-generated legal/forensic determination.

This is **not** automated proof validation, a professional laudo, or a finding that a record was altered, omitted or inadmissible.

### Why might anyone pay?

**Business hypothesis:** a bounded scope could reduce paid professional time spent just assembling, cross-referencing and determining which documents need requesting, while enabling earlier expert scoping and avoiding unnecessary full audits. It competes with **free checklists**, internal manual work and **R$ 800 human triage**; the public price anchor shows a service offer exists, **not** that a software buyer will pay or that demand is high.

**Potential differentiated combination (not verified unique):** *machine-readable claim-to-source/absence ledger + user-controlled execution + explicit epistemic boundaries + expert-ready handoff*, initially for report-first receipt and only later for complete UFDR reprocessing. All parts must be compared against the actual user workflow, not against vendor marketing text alone.

## Smallest falsification experiment (no production code)

1. **Find the trigger:** ask three professionals about their *last* received report before showing any package. Count whether they were unable to tell what to request/verify, and whether this caused delay, money or material risk.
2. **Show two synthetic outputs:** (A) ordinary document checklist/template, (B) claim-to-source/absence ledger with explicit verification boundaries. Same fictional input, same time budget. Blind the product name if practical.
3. **Compare alternatives:** ask them to complete the task in their current tools (including IPED/Reader/Cyberlex/free checklists where actually accessible). Document **actual unmet step** rather than features guessed absent.
4. **Observe behavior:** will a real budget owner schedule a scoped **paid** test on authorized/synthetic material, identify budget and approver, and state acceptance criteria? Do not count praise or willingness to receive updates.
5. **Stop condition:** if two participants can do the same job accurately in their existing workflow for negligible cost, or the issue rarely matters, **do not prioritize**. If the expensive step is actual source-level forensic validation rather than document review, **pivot** to an expert-assisted service instead of misrepresenting the software.

## Candidate scoped paid pilot only if interviews support it

**One anonymized/public synthetic scenario**, limited to *one report and its listed evidence attachments*, and a **fixed number of material claims** agreed in advance. Deliver a reviewer-signed checklist of available/unavailable references, not a forensic opinion. Manual preparation is acceptable at this stage; measure preparation time, review time, corrections, confidence and usefulness against a plain document template. Define price and safeguards **with the buyer**, not by inventing the buyer's budget.

No real case material is authorized for processing by this research. Before any later confidential pilot: lawful authority, NDA/contract, access controls, hosting/data retention rules, redaction, incident procedure and qualified human-review scope.

## Sources (public, read 2026-11-15)

1. [STJ — AgRg no AgRg no HC 1.041.340/RS (15 Apr 2026)](https://scon.stj.jus.br/SCON/pesquisar.jsp?O=JT&b=ACOR&i=1&livre=%28%27AAHC%27+INPATH%28CLAS%29+AND+%271041340%27+INPATH%28NUM%29%29+OR+%28%28%27AgRg+no+AgRg+no+HC+1041340%27%29+INPATH%28SUCE%29%29)
2. [STJ — Informativo 878, AgRg no HC 1.014.212/ES](https://scon.stj.jus.br/jurisprudencia/externo/informativo/?acao=pesquisarumaedicao&from=feed&livre=0878.cod.)
3. [STJ — Edição Extraordinária 33 (28 Jul 2026); fact-specific dissent from generic nullity](https://scon.stj.jus.br/jurisprudencia/externo/informativo/?acao=pesquisarumaedicao&livre=%270033E%27.cod.)
4. [NIST IR 8354 — Scientific Foundation Review](https://nvlpubs.nist.gov/nistpubs/ir/2022/NIST.IR.8354.pdf)
5. [STJ — Informativo 884, HC 1.059.475/SP](https://processo.stj.jus.br/jurisprudencia/externo/informativo/?acao=pesquisar&aplicacao=informativo&livre=%40CNOT%3D%27022245%27)
6. [Cellebrite Reader](https://cellebrite.com/en/products/cellebrite-inseyets/reader/); [Guardian](https://cellebrite.com/en/products/guardian/)
7. [IPED — upstream README](https://github.com/sepinf-inc/IPED)
8. [Oxygen Forensic Detective](https://www.oxygenforensics.com/products/oxygen-forensic-detective/)
9. [DefendaMe — advertised scope](https://defendame.com/)
10. [Alarion — phone extraction review](https://alarion.ai/phone-extraction-review.html)
11. [Cyberlex — published service offerings and pricing](https://cyberlex.app/advogados)
12. [Criminal Player — locally executed custody toolkit](https://visite.criminalplayer.com.br/ajuda-kit-cadeia-de-custodia/)

**Recommended next action:** test the *reviewability gap* task against a plain checklist/template and named substitutes before any VIII ingestion or report-building milestone. Research alone is not sufficient to mark #6 done.
