# Service hypothesis: assisted workstation setup and capability enablement

**Status:** 2026-11-15 — exploratory commercial hypothesis for [issue #6](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/6). **No customer interviews, paid setups, or measured margins are claimed.** This is not a roadmap commitment or a current VIII feature.

## Core proposition

VIII can remain **fully open-source and self-service**, while Luminiware optionally charges to **configure, test, maintain, and support a more capable evidence-review workstation** for people who do not want to administer developer tooling.

The same source code and supported capabilities must remain freely usable by anyone with the necessary hardware, dependencies, permissions and any separately licensed third-party services. **Do not cripple the free setup or withhold essential features to create demand for consulting.**

**Customer job:** “I want to review digital evidence using powerful tools without configuring Docker, local processing engines, agents, credentials or execution infrastructure myself.”

**What a customer purchases:** a configured and verified environment, training, bounded support, and optionally an ongoing maintenance contract—not access to the VIII code.

## Capability profiles (not paid editions)

| Suggested profile | Free self-service path | Optional implementation work | Boundaries |
| --- | --- | --- | --- |
| **Essential** | Install VIII and use the natively supported tasks with no Docker requirement | Install, orient users, validate machine and supported format flow | No implemented general case-processing workflow yet; don't claim these features ship |
| **Advanced** | User enables Docker-compatible adapters, container workers, and eventually distributed compute | Check platform/WSL/virtualization/resources, install suitable container runtime, set up integrations, run synthetic acceptance tests, document resource needs | Docker must be optional. Docker Desktop has distinct licensing from Docker Engine; Bacalhau requires a compatible configured execution environment |
| **Assisted** | User enables an optional, permissioned agent adapter (Codex CLI is a **candidate**, not an approved forensic tool) | Install/configure provider, sandbox, credentials, permissions, approvals, data egress policies and test with **synthetic** material | A local CLI may use remote models and transfer inputs. No unattended evidentiary or legal conclusions, no unrestricted host/file access |

Profiles describe **capabilities detected**, not commercial subscription levels. A user may mix local, container and optional agent integrations; an integration's activation is never mandatory for ordinary use.

## Open feature proposal: VIII Environment Doctor

A diagnostic feature/CLI, eventually offered to **everyone**:

1. Detect OS, architecture, storage/RAM/CPU/GPU constraints, native dependencies and permission availability without silently modifying the host.
2. Report available **native** capabilities, container runtime and versions, optional Bacalhau executor health, and optional agent provider status.
3. Show capability-to-prerequisite mapping with exact missing items, compatibility and user-facing steps for self-installation.
4. Perform **synthetic-only** execution tests and report whether outputs, source references and restrictions behave as expected. Capture versions/config fingerprints; never retain credentials or sensitive evidence in diagnostic logs.
5. Explain execution location, outbound network/data handling, third-party costs/license requirements, and the need for explicit approval before remote jobs or agent input sharing.
6. Generate a minimal, user-owned environment report usable for DIY installation or as an authorized scope of work for **any** provider, including Luminiware.
7. Where safe, offer one-click setup only with reviewable changes and explicit confirmation; no root/admin privileges or Docker-socket access by default.

**Critical promise:** a free path to follow the diagnosis and enable exactly the same features yourself. The paid installation removes effort, risk and uncertainty, **not software rights**.

## Candidate Luminiware service catalog (prices are exploration ranges only)

| Offer | Hypothetical public price for testing (BRL) | Deliverables / acceptance | Revenue |
| --- | ---: | --- | --- |
| Essential workstation setup | **R$ 300–600** | Preflight compatibility, installed native environment, synthetic smoke test, handover and concise guide | One-off |
| Advanced workstation setup | **R$ 800–1,500** | Runtime/container setup as applicable; one or two named tool integrations, permission checks, synthetic processing run, usage guide | One-off |
| Assisted agent setup | **R$ 800–2,000** | Optional provider authentication, sandbox/approval/egress policies and clearly scoped **non-evidentiary** tasks tested with synthetic inputs | One-off |
| Workstation maintenance and support | **R$ 150–400/month** | Version updates, health checks and defined support window/SLA (to negotiate); not unlimited incident response | Recurring, if the need exists |

These ranges come from an exploratory planning exercise, **not** a verified market study, customer quote, or promise of service availability. Hardware, third-party licenses, Docker Desktop subscriptions where applicable, cloud hosting and model/token usage are outside the service fee unless separately contracted.

A setup sold for **R$ 900** that takes **3 labor hours** yields **R$ 300 gross per labor hour before taxes/overhead**; at **10 hours**, **R$ 90**. These examples are simple illustrations, not projected margin or revenue. Track setup, pre-sales and post-handover hours to assess unit economics.

## Commercial differentiation

- **VIII as acquisition/distribution:** free, inspectable, portable software and a public capability diagnostic lower the barrier to trying a narrow review task.
- **Setup as first service:** offer a *working, tested environment* with minimal user effort, not “Docker installation” as an end in itself.
- **Trust as retention:** document how data stays local or travels, who can access files, how updates work and how to leave Luminiware without losing data/capabilities.
- **Expansion path:** optional recurring workstation support, BYOC operations, custom integrations and independent expert review **if** the buyer has that need.
- **Counterargument:** a competent IT contractor can install Docker or Codex. A defensible paid offer must include evidence-specific acceptance tests, compatibility and safeguards, workflow integration and practical handover—not just commodity installation.

Two different buyer pathways may exist: (A) independent lawyer/small practice buying one setup, (B) firm/lab IT or operations leader buying maintenance at scale. Validate purchasing authority independently.

## Smallest experiment (no VIII implementation required)

**Goal:** establish willingness to pay for installation/configuration of an *open-source digital-evidence workstation*, using currently available and properly licensed tools on synthetic/public permitted fixtures. Don't imply the VIII application is operational.

1. Recruit **three relevant prospective customers**, preferably users lacking in-house technical support; screen for OS, hardware, technical comfort, actual repeated task, existing tool and data governance needs.
2. Offer a **fixed-scope paid setup** (or clearly distinguish a free observation from a paid commitment). Explain what dependencies are supplied by customers and what is out of scope.
3. Before work, record the buyer, actual accepted price, purchasing path, expected output and synthetic acceptance test; obtain authorization for machine configuration.
4. Deliver installation and a handover exercise in which the customer independently performs a **permitted synthetic** task. Capture total setup time, troubleshooting, user completion time, follow-up incidents, support hours, third-party cost and perceived alternatives.
5. Invite optional ongoing support; treat a separate paid maintenance discussion or contract as **additional** evidence, not an automatic extension of the setup purchase.

### Decision criteria

- **Proceed with a repeatable setup service:** at least one qualified person actually purchases (or commits through an actionable purchasing step) a specified setup, the synthetic acceptance test is passed, and time/cost show plausible gross contribution after expected support.
- **Pivot:** the buyer values a professionally reviewed *output* but will not pay to operate a workstation; explore expert review or a managed service instead.
- **Do not prioritize:** technical users can easily self-install, the practical problem does not recur, buyers reject setup pricing, or support burden overwhelms expected margin.

These are deliberately small **decision rules, not product-market-fit proof**. A prior user-requested three-interview discovery threshold remains applicable to the broader issue #6; a single paid setup does not close that issue by itself.

## Security, legal and technical gates

- The user owns source material and credentials; obtain explicit permission for each setup/change. Never collect or commit client secrets or real forensic exports in a public issue or demo.
- Docker daemon/socket access is privileged. Favor least privilege, isolated containers, tested resource limits, verified image sources, and explicit mount/network policy.
- Docker Engine's open-source licensing and Docker Desktop's commercial use terms differ. Check the customer's actual organizational licensing requirements.
- Codex CLI is an optional programming agent with different model/data handling depending on provider and settings. Its local presence is **not** local inference, air-gapped processing, or forensic validation. Consider prompt injection, shell/file system permissions, tool approvals and remote data transfer.
- Bacalhau can be an optional remote/distributed executor in a future design. Do not make it a local installation prerequisite or claim it inherently enforces confidentiality/compute budgets.
- Explicitly test offline operation and third-party dependencies before claiming 100% open-source/offline usability.
- No secret-of-justice material or real-case analysis in this experiment without separate authorization, privacy/security controls and appropriate professional boundaries.

## Product separation and next actions

**For VIII (open source):** only propose an Environment Doctor once concrete installation friction is observed. Build the smallest capability detection + synthetic smoke test before proposing automated provisioning.

**For Luminiware (optional service):** write a narrow installation statement of work, publish a transparent price **after validation**, create repeatable checklists and handover tests, and measure support economics. Services may exist independently of an VIII commercial edition.

**Decision now:** keep this as a separately testable **commercial hypothesis** that complements, but does not substitute for, finding a differentiated evidence-review task in #6.

## References

- [Issue #6 — task-level paid-pilot discovery](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/6)
- [Service-only open-source model](open-source-services-model.md)
- [Differentiation hypothesis](differentiation-hypothesis.md)
- [Bacalhau documentation](https://docs.bacalhau.org/)
- [Docker Desktop license terms](https://www.docker.com/pricing/faq/)
- [Docker daemon attack surface](https://docs.docker.com/engine/security/)
- [OpenAI Codex CLI source](https://github.com/openai/codex)
