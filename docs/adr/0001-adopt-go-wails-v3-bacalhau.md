# ADR-0001: Adopt the Bacalhau runtime as the job orchestrator

- **Status:** Accepted
- **Date:** 2026-11-15
- **Accepted:** 2026-11-16 — project maintainer decision.
- **Revised:** 2026-11-16 — restrict this decision to the job runtime and orchestration boundary.
- **Decision scope:** Bacalhau runtime for local and remote job orchestration
- **Project:** VIII — Digital Evidence Workbench

The application stack is a separate decision in [ADR-0002: Go + Wails v3](0002-adopt-go-wails-v3.md). The existing filename is retained for incoming links.

## Context

VIII aims to make digital evidence analysis accessible to nontechnical users while preserving traceability, operational sovereignty, and extensibility.

The product is expected to evolve through multiple deployment scenarios:

1. **Local workstation:** a single computer manages and executes processing jobs.
2. **Local network:** additional workstations contribute processing capacity.
3. **Bring Your Own Cloud (BYOC):** processing runs on infrastructure controlled by the customer.
4. **Managed cloud:** processing runs on infrastructure operated by an authorized service provider.
5. **Hybrid deployments:** selected workloads execute across authorized local and remote environments.

These scenarios should not require separate products or fundamentally different processing interfaces.

The architectural challenge is to minimize both initial implementation complexity and long-term operational complexity.

A solution optimized exclusively for local execution may require substantial redesign when distributed processing becomes necessary. Conversely, introducing a complex distributed infrastructure too early may undermine the project's low-ops requirements.

An additional concern is avoiding multiple overlapping orchestration systems. Combining a general-purpose job framework with Bacalhau could introduce redundant scheduling, retry handling, monitoring, and lifecycle management.

The synthetic PoC demonstrated a substantial performance advantage for direct native Go execution. The revision deliberately accepts additional local runtime cost to preserve one job lifecycle and a portable worker path across the roadmap. The [native execution trade-off case](0001-native-execution-tradeoff.md) records the measurements, graphs, alternatives, rationale and unresolved costs. This is an architectural direction, not evidence that the operational acceptance gates have passed.

## Decision drivers

The MVP architecture should prioritize:

1. **Low operational overhead:** installation, configuration, upgrades, troubleshooting, and recovery should require minimal manual intervention.
2. **Simple distribution:** preferably a small number of self-contained executables, without requiring users to install development runtimes.
3. **Local-first operation:** supported workflows must function without external services.
4. **Natural distribution path:** support adding compute nodes without replacing the job execution architecture.
5. **Single orchestration authority:** avoid competing systems managing the same processing lifecycle.
6. **Reuse of existing tools:** prefer established forensic software rather than rebuilding equivalent capabilities.
7. **Evidence traceability:** preserve source references, processing metadata, and reviewability across execution environments.
8. **Open-source compatibility:** avoid mandatory proprietary infrastructure or features restricted to commercial editions.
9. **Maintainability:** favor established frameworks and small integration surfaces over custom infrastructure.
10. **Accessible UX:** nontechnical users should not need to understand containers, distributed computing, or command-line tools.

## Decision

Use **the Bacalhau runtime as the sole orchestrator for local and remote evidence-processing jobs**. Its common job lifecycle, placement and execution capabilities are advantageous for the local-to-LAN/BYOC roadmap: VIII can reuse a job runtime instead of building and maintaining its own orchestration infrastructure.

This decision applies independently of VIII's application language, desktop framework and worker implementation languages. The measured local runtime costs are accepted for that architectural benefit; lower total operating and engineering cost remains to be demonstrated.

Use **WASI as the portable, Docker-free option for compatible workers** and **Docker as the performance-oriented option for native workers and specialized dependencies**. Docker remains an optional compute-node capability; basic workstation use must not require it.

**Direct in-process Go jobs and direct native subprocess jobs are excluded from the product execution architecture, including as automatic fallbacks.** Go code may still compile to a native executable inside a Docker worker. Native benchmark paths remain useful comparison tools.

### 1. Processing orchestration: Bacalhau

Bacalhau will be responsible for:

- Job submission and lifecycle management.
- Scheduling and execution placement.
- Resource-aware execution.
- Execution status and logs.
- Retry and failure handling within supported guarantees.
- Coordinating authorized compute nodes.
- Executing Docker and WebAssembly workloads.

The application will communicate with Bacalhau through a dedicated integration boundary.

**VIII must not implement a competing durable processing queue or scheduler in the MVP.**

Ordinary application operations, including input validation, hashing, persistence, search and presentation, do not need to become Bacalhau jobs. Evidence-processing jobs use Bacalhau regardless of duration or execution location; a short job does not gain a separate native lifecycle. An unavailable runtime must produce a recoverable diagnostic rather than silently bypassing this boundary.

### 2. Execution environments

The initial architecture will recognize two Bacalhau execution mechanisms:

**WebAssembly / WASI — portability**

- Preferred for compatible workers when portability and Docker-free operation are required.
- The same compatible module can execute on heterogeneous authorized nodes without per-OS native worker packaging.
- Does not require Docker.
- Limited by the capabilities of Bacalhau's supported WASI runtime.
- Requires actual workload compatibility and latency testing. The PoC measured significant compilation and arithmetic execution costs; portability does not imply native performance.

**Docker — performance and native dependencies**

- Preferred when CPU cost, GPU or native dependencies justify a container runtime; performance must be verified for the workload.
- Workers may reuse domain logic across WASI modules and native executables inside architecture-specific images where their toolchains support both targets.
- Suitable for existing forensic tools and specialized processing environments.
- Enables reuse of containerized OCR, media-processing, parsing, and indexing tools.
- Requires a functioning Docker-compatible environment on the compute node.
- May introduce installation, licensing, security, and maintenance requirements.

Docker is a capability dependency, not a commercial feature gate.

Users remain free to configure supported execution environments independently.

### 3. Local-first topology

The initial Bacalhau installation will use a local hybrid node, combining orchestration and computation.

The proposed deployment consists of:

- The VIII desktop application.
- A locally managed Bacalhau process.
- The required processing modules or container images.
- Local evidence and application storage.

Adding remote compute nodes should not require rewriting the application's job contracts.

Local-first means execution can remain entirely on the workstation; it does not mean processing jobs bypass Bacalhau. Local and remote placement must be explicitly authorized before evidence is staged or submitted. A shared job contract does not make remote evidence transfer automatic.

The installation experience must hide orchestration details from nontechnical users.

### 4. Evidence and execution boundaries

VIII will own the domain model:

- Cases and evidence records.
- Original artifact identification and integrity metadata.
- Processing requests and their relationship to cases.
- Source-linked observations and derived artifacts.
- Human review status.
- Execution policies and authorization decisions.

Bacalhau will own the execution lifecycle of submitted jobs.

A job identifier alone is insufficient as a forensic provenance record.

VIII must independently retain the relationship between inputs, processing configuration, tool versions, outputs, and source references.

Remote execution must not imply automatic authorization to transfer evidence outside the customer's environment.

## Integration boundary

VIII's application layer authorizes evidence placement, submits jobs through a Bacalhau adapter, reconciles attempts and verifies returned artifacts. Bacalhau supplies the local or remote orchestrator and authorized compute nodes using WASI or Docker workers. The application implementation and persistence technology are separate decisions.

Adding a compute node must not automatically make evidence accessible to it.

## Update and maintenance strategy

The MVP must support controlled updates when no evidence-processing jobs are active.

The intended update workflow is:

1. Enter maintenance mode and reject new processing requests.
2. Confirm that no active or pending jobs would be disrupted.
3. Preserve application state and required rollback information.
4. Download and verify versioned updates.
5. Update the Bacalhau runtime and workers, coordinating with application compatibility as needed.
6. Restart affected processes.
7. Run automated health checks and synthetic processing tests.
8. Resume normal operation only after successful validation.

The application layer's update mechanism does not automatically cover Bacalhau, Docker or workers. Runtime/worker compatibility and component update coordination remain VIII responsibilities; selecting the application's updater is outside this ADR.

Hot code swapping is not a requirement. Controlled restart during an idle maintenance window is acceptable.

## Alternatives considered

### Application-managed job orchestration

**Advantages**

- Full control over execution semantics.
- No external orchestration dependency.

**Trade-offs**

- Requires implementing and maintaining scheduling, retries, cancellation, monitoring, and eventual distributed execution.
- Increases the risk of building infrastructure unrelated to the core forensic value proposition.

**Assessment:** not selected unless Bacalhau fails the MVP acceptance criteria.

### Bacalhau with a custom native executor

A custom native executor could support directly invoking tools without Docker.

However, it would introduce additional engineering and security responsibilities, as well as a compatibility burden across Bacalhau versions.

**Assessment:** explicitly excluded from the initial MVP.

### Direct native Go locally, Bacalhau remotely

**Advantages:** the measured native paths have much lower latency, avoid local scheduler/staging processes and their installation/update burden, and can reuse the same Go normalization logic. Native execution can preserve source references and provenance; it is not inherently incompatible with distributed processing or evidence integrity.

**Trade-off:** this introduces two execution paths whose lifecycle behavior, artifact packaging, cancellation, recovery and provenance must remain consistent. A synchronous native call is small today; making it support durable long-running product jobs still needs lifecycle decisions. Shared Go code and a common interface can reduce duplication, but do not erase the different runtime and failure behavior. Bacalhau also leaves application-level reconciliation and evidence policy to VIII.

**Assessment:** rejected for product jobs. The project prioritizes a common local-to-LAN/BYOC job model and portable worker distribution over the fastest isolated local execution. Native code remains available inside Docker workers and in benchmark baselines. The expected lifecycle/engineering benefit of this choice is not yet measured. See the [technical case and evidence](0001-native-execution-tradeoff.md).

## Consequences

### Positive

- Bacalhau offers a potential evolution from local to distributed execution.
- A single orchestration system reduces duplicated responsibilities.
- Local and remote jobs share an execution boundary, independently of the application stack.
- Existing containerized forensic tools can be reused.
- The application can expose the same processing concepts across deployment modes.
- Managed installation and maintenance become well-defined optional service opportunities.

### Negative

- Bacalhau remains a separately managed process.
- Every workstation pays for runtime management and worker distribution, even when all jobs stay local.
- Direct native execution's measured latency advantage and simpler local runtime footprint are deliberately forgone; WASI startup and compute costs can affect interactive workflows.
- A single application executable does not mean a single-component deployment.
- The Docker dependency may complicate Windows installations.
- WASM compatibility limitations may restrict Docker-free workflows.
- Multiple application and worker versions introduce compatibility-management responsibilities.
- Distributed execution introduces data-placement, security, and networking complexity.

## Validation requirements

The architecture decision is **Accepted**. The following requirements remain implementation and operational validation work; acceptance does not assert that they have passed or that the product is production-ready. Outstanding work remains tracked in [issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9).

The proof of concept must validate:

These requirements assess runtime integration and its operational cost. Desktop shell, frontend usability and application-stack packaging validation belong to ADR-0002; a failure there does not by itself invalidate this runtime choice.

### A. Runtime installation and application integration

- Package the Bacalhau runtime and compatible workers for the initial Windows workstation target.
- Run the runtime and basic Docker-free worker without development tools installed.
- Have the application start or connect to the local Bacalhau process.
- Present meaningful diagnostics when required dependencies are missing.
- Complete installation and first use without requiring command-line interaction from the end user.

### B. Local forensic workflow

- Import a small, synthetic UFDR XML fixture.
- Execute a compatible processing worker through Bacalhau.
- Normalize supported messages.
- Return results with source-record references through the application boundary.
- Preserve execution metadata and original evidence integrity.

The Docker-free path must be tested rather than assumed. If a suitable WASM implementation is unavailable or disproportionately expensive to maintain, this must trigger reassessment.

### C. Distributed execution

- Add a second compute node.
- Submit the same logical job through the existing application interface.
- Execute it on an explicitly authorized node.
- Preserve provenance and execution history.
- Demonstrate correct data placement and authorized access.

### D. Failure recovery

- Terminate a running worker unexpectedly.
- Verify how Bacalhau reports and handles the failure.
- Restart the application and recover persisted processing state.
- Test cancellation and retry behavior.
- Ensure retries do not create uncontrolled duplicate evidence records.

### E. Updates

- Update the runtime during an idle period without losing application/job state.
- Update at least one worker independently.
- Validate integrity, compatibility, and restart behavior.
- Test a failed update and rollback.
- Confirm that previously produced results remain traceable to the versions that created them.

### F. Operational cost

Measure:

- Clean installation effort.
- First-job success rate.
- Idle resource consumption.
- Job execution overhead.
- Update and recovery effort.
- Number of separately managed components.
- Additional configuration required to introduce a second node.

Retain the minimal native Go baseline for measurement, without implementing a competing scheduler or exposing it as a product execution mode. Measure lifecycle and incremental LAN/BYOC engineering cost as well as CPU time; the choice of a common runtime does not establish that its total cost is lower.

## Operational readiness criteria

The accepted architecture must demonstrate the following before operational readiness can be claimed:

1. A nontechnical user can install and execute the basic workflow without manually configuring orchestration infrastructure.
2. The same processing contract supports local and authorized remote execution.
3. Bacalhau provides reliable job lifecycle management without substantial custom scheduling code.
4. The runtime and workers can be safely updated during idle periods, preserving application compatibility and job history.
5. Operational complexity remains acceptable for a single workstation.
6. Source-linked results remain verifiable independently of the execution location.
7. Docker is not required for any workflow explicitly advertised as Docker-free.

No performance, deployment, or reliability claims will be considered validated without reproducible tests.

## Reconsideration triggers

Revisit this ADR if:

- Bacalhau requires a substantial custom executor for priority workflows.
- Docker-free processing introduces disproportionate engineering cost.
- Measured first-use latency, footprint or lifecycle cost is unacceptable for the validated workflow. The already-known extra components relative to native execution are an accepted trade-off, not a new finding by themselves.
- The runtime cannot be installed, managed or updated acceptably on the target workstation.
- Bacalhau's distribution model cannot satisfy the project's data-locality and authorization requirements.
- Another architecture demonstrates materially lower lifecycle cost while preserving the roadmap.

## Out of scope

This ADR does not decide:

- The application language or desktop framework; see [ADR-0002](0002-adopt-go-wails-v3.md).
- Worker implementation languages, beyond compatibility with the selected execution engines.
- The complete frontend component framework.
- The final database or search engine.
- The cloud infrastructure provider.
- Multi-tenant architecture.
- The complete forensic tool catalog.
- AI model providers.
- Commercial pricing or support contracts.
- The final packaging strategy for Docker Desktop.

These decisions should follow specific product or technical requirements.

Adding direct native job execution would require revising this decision; it is not a deferred adapter within the selected architecture.

## References

- [Technical case: direct native execution trade-off, graphs and artifacts](0001-native-execution-tradeoff.md)
- [ADR-0001 follow-up: distributed runtime validation](0001-follow-up-distributed-runtime.md)
- [ADR-0002: Go + Wails v3 for the VIII application layer](0002-adopt-go-wails-v3.md)

- [VIII — Digital Evidence Workbench](https://github.com/vmenezesdev/viii-digital-evidence-workbench)
- [Bacalhau architecture](https://bacalhau.org/docs/overview/architecture/)
- [Bacalhau execution engines](https://bacalhau.org/docs/references/faq/)
- [Docker Desktop licensing](https://docs.docker.com/subscription-billing/desktop-license/)

## Decision summary

Adopt **the Bacalhau runtime as VIII's job orchestrator** for its common lifecycle and execution capabilities across the local-to-distributed roadmap. This architecture decision is accepted; operational validation remains pending.

Use Bacalhau for local and remote jobs, WASI for portable Docker-free workers, and optional Docker workers where performance or dependencies justify them. Direct native job execution outside this runtime is excluded. The application stack is decided separately.

The decision prioritizes **one execution model across the local-to-distributed roadmap**, accepting measured local performance costs. Lower total lifecycle complexity remains a hypothesis to validate. ADR-0001 is **Accepted**; implementation alignment and operational readiness remain subject to the requirements above and the reconsideration triggers.
