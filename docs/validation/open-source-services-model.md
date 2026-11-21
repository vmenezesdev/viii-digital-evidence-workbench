# Can a fully open-source VIII monetize services?

**Desk-research note — 2026-11-15. Hypothesis, not customer-validated.** This tests a *business model*, not a newly discovered forensic need.

## Answer

**Yes: a fully open-source core with optional professional services is legally and commercially feasible.** This is supported by public examples of actual companies selling services around free/open software. **No: open source or customer-controlled hosting alone is not a defensible differentiator**, because credible open-source forensic tools already exist. Treat openness as a *trust/procurement and distribution enabler* and compete on **which costly professional workflow is made easier**.

Possible externally legible proposition:

> **Your evidence, your infrastructure, your control. Open-source software, with expert help when you need it.**

Do **not** claim "zero knowledge", independently verified custody, guaranteed confidentiality, production readiness, or independence from every external provider merely because source code is public.

## Verified analogues (not proof of demand for VIII)

| Project / company | Publicly verified proposition | Implication |
| --- | --- | --- |
| **Autopsy / Sleuth Kit Labs** | Autopsy is free/open-source; company advertises **subscription-based support, training, and custom modules**; current training catalog lists an 8-hour course for **US$495**. [1][2] | **Direct forensic-domain precedent** that a free tool can coexist with paid professional services. Catalog price is not evidence of booked revenue or margin. |
| **Wazuh** | Free/open-source security platform with customer-hosted and **vendor-operated cloud** options; also advertises professional support, training and consulting. [3] | A non-forensic operational precedent for paying for *management and accountability*, not a code license. |
| **IPED** | Open-source GPLv3 forensic tool created by the Brazilian Federal Police, with broad media processing and review functionality. [4] | **Counterevidence to novelty** of “open-source digital forensics in Brazil”. |
| **OpenRelik** | Apache-2.0 open-source digital-forensic workflow server with artifact sharing, extensible workers and deployment in customer cloud/local infra. [5] | **Counterevidence** to novelty of “open source + cloud deployment + collaboration”. |
| **Timesketch** | Apache-2.0 collaborative forensic timeline analysis. [6] | Open timelines, tags and collaboration alone are not differentiators. |

## What could be differentiated (still hypothetical)

A narrower **report-first defense workflow**: with only the material *actually furnished*, help counsel record what can be traced to a supplied source, what is only asserted by a report, what remains missing or uninspectable, and which checks require a qualified independent specialist. Then offer optional deployment, training, operations, and scoped expert review.

Openness could make this promise credible if VIII offers:
- An **unrestricted free path** to run the same essential review tasks, with documented export formats and no forced vendor account, upload or phone-home. Test offline behavior; do not assert it beforehand.
- Auditable source-to-finding references and mechanical checks clearly separated from professional interpretation.
- **Customer-controlled deployment**, including either local operation or BYOC, without assuming that cloud ownership alone implies confidentiality/security compliance.
- Portable data and ability to leave the service provider without losing case work products or capabilities.
- Optional services that reduce actual work or procurement risk, not functionality withheld behind a closed feature flag.

These are **proposed guarantees to implement and verify**, not existing capabilities. Buyers may care more about an accurate reviewed output than whether the tool is open source; test both motives independently.

## Service-only commercial offers worth testing

| Offer | Potential buyer / trigger | Value delivered | Charging logic (hypothesis) | Caution |
| --- | --- | --- | --- | --- |
| **Setup + training** | Specialized practice or forensic lab starting repeat work | Install/configure, train team, documented workflow and handoff | Fixed project | One-off revenue; needs repeatable playbook. |
| **BYOC operations + response SLA** | Team with recurring workloads and infrastructure owner | Upgrades, hardening, troubleshooting, backups, monitoring, transparent workload budgets | Monthly/annual contract, customer pays its own cloud | Credible SLA, security and operational maturity required; not MVP. |
| **Per-matter specialist review** | Criminal counsel needing bounded preliminary review | Independent qualified human scrutiny of a source-reference/limits package | Per authorized scope | Labor scales with volume; no claim that software issues expert opinions. |
| **Custom connectors and enterprise integration** | Institution with repeated format/data constraints | Custom inputs, access policies, export integration, documentation | Implementation + optional maintenance | Check third-party licenses, provenance assumptions, and permission to handle data. |

**Initial commercialization priority:** interview for the **per-matter work product**, because it aligns with the immediate professional task and can be manually demonstrated. Ask separately whether the **same buyer** would pay for ongoing operations: do not conflate a lawyer purchasing a one-off review with an IT budget owner purchasing support MRR.

## Licensing and commercial constraints

1. **Open source can be commercial.** OSI explicitly permits selling services, support, warranties, customization, and maintenance; it also permits competitors to offer the same software or services. A business model must not depend on preventing forks. [7]
2. VIII currently has a root **MPL-2.0 LICENSE** file, though its English README still says the license is “planned” and files will be added — a documentation inconsistency to fix in a *separate small change*. The mere existence of LICENSE does not establish that a future full stack is 100% open. [8]
3. **MPL 2.0 permits commercial use** and imposes file-level source-sharing obligations for covered modifications upon distribution, while allowing larger works with separately licensed files; it does **not** force external service vendors to send money or contributions upstream. [9]
4. Do **not** combine IPED GPL code or other dependencies into a single redistributed work without evaluating actual licenses, linking and applicable compatibility; subprocesses and network integrations need their own careful analysis. Software composition, containers, OCR engines, model weights, firmware, format reader SDKs and service APIs all have independent terms.
5. A truthful **“100% open-source software”** claim needs inventory of essential shipped/runtime components (SBOM + notices), reproducible self-hosting and absence of mandatory proprietary APIs. Optional proprietary cloud LLMs can exist, but are **not** part of a 100%-open execution path. "Open weights" is not automatically open-source software.
6. **Licensing does not guarantee security/privacy.** For Brazilian personal data, actual roles and liability under LGPD depend on who determines purposes, who processes data and how the engagement operates; access by the service provider and subcontractors must be mapped. Client-hosted infrastructure alone does not eliminate obligations. [10]

## Commercial risks

- The product may become a **free substitute for other free software**, without meaningful switching incentive.
- Experts may prefer IPED/Autopsy/OpenRelik; counsel may prefer a paid expert opinion without operating any software.
- Free software is not zero-cost to support: installation, security assurance, docs and complex evidence formats create ongoing burden.
- Paying users can hire other providers to deploy the same software. This is a deliberate **no-lock-in** tradeoff, compensated only by quality, reputation, speed, domain workflow, partner network and contracts.
- Services are more labor-intensive than software licenses; true recurring revenue requires a repeated need, support contract, verified operational responsibility and realistic margin.

## Smallest commercial falsification test

Three interviews already required by issue #6; add these **non-leading questions** after reconstructing the last real task:

1. “What would make you select this workflow over IPED/Autopsy/Reader, a checklist or a forensic consultant? Does access to the source code materially change that choice?”
2. “Who would *run* it, who would review the output, and who would pay? Are those the same person?”
3. “If the exact software were free and could run on your machine, which *specific service* would you still purchase: setup, urgent support, managed operation or specialist review? What was your last comparable spend?”
4. “Would you accept a provider with no access to the evidence, or require someone to process/review the actual content? Which contract or safeguard would that trigger?”
5. Show an open-source self-service path and a paid *human-review* path for the **same synthetic example**. Seek a real, explicitly paid scoping meeting and budget route, not a generic preference for open source.

**Proceed only if** at least one qualifying buyer names a specific purchasable service, budget and concrete follow-up, and the problem itself passes the issue's task-level checks. If users value open code but will not pay for the offered services, this is an attractive **open-source project**, not yet a proven service business.

## Decision for #6

**Keep fully open-source as a strategic constraint / trust enabler, not as the single differentiator.** The differentiator still needs to be a superior, narrower defense review task and a validated paid service around it. Avoid a licensing overhaul or service platform build before real evidence.

## Sources

1. [Autopsy, official site — free/open and paid training/support/custom modules](https://www.autopsy.com/)
2. [Sleuth Kit Labs training catalog](https://training.sleuthkitlabs.com/collections/autopsy)
3. [Wazuh Cloud deployment models](https://documentation.wazuh.com/current/cloud-service/index.html); [Wazuh support and consulting](https://documentation.wazuh.com/current/getting-started/index.html)
4. [Inter-American Development Bank — IPED overview and GPLv3](https://knowledge.iadb.org/pt-br/conhecimento-aberto/codigo-para-desenvolvimento/solucao-de-codigo-aberto/iped-digital-forensic-tool)
5. [OpenRelik documentation](https://openrelik.org/docs/); [server repository Apache-2.0](https://github.com/openrelik/openrelik-server)
6. [Timesketch project](https://github.com/google/timesketch); [Apache-2.0 license](https://github.com/google/timesketch/blob/master/LICENSE)
7. [Open Source Initiative FAQ — commerce and services](https://opensource.org/faq)
8. [VIII current LICENSE](https://github.com/vmenezesdev/viii-digital-evidence-workbench/blob/trunk/LICENSE) and [README](https://github.com/vmenezesdev/viii-digital-evidence-workbench/blob/trunk/README.md)
9. [Mozilla MPL 2.0 FAQ](https://www.mozilla.org/en-US/MPL/2.0/FAQ/)
10. [Brazil ANPD — data controller/operator roles](https://www.gov.br/anpd/pt-br/assuntos/titular-de-dados)
