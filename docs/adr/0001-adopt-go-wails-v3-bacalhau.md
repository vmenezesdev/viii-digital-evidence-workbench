# ADR-0001: Adopt Go, Wails v3, and Bacalhau for the MVP

- **Status:** Proposed
- **Date:** 2026-11-15
- **Decision scope:** MVP technology stack and execution architecture
- **Project:** VIII — Digital Evidence Workbench

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

We propose adopting **Go as the application backend, Wails v3 as the desktop application framework, and Bacalhau as the sole orchestrator for evidence-processing jobs**.

Docker will remain an optional execution dependency for workflows that require containerized tools.

### 1. Application layer: Go + Wails v3

The VIII application will use:

- Go for application services, domain logic, evidence management, and integrations.
- Wails v3 for native desktop packaging and interaction with a web-based frontend.
- A lightweight HTML/CSS/TypeScript frontend, with the specific UI framework selected independently.

Wails provides desktop capabilities without requiring a separate application backend runtime.

Its server build mode also provides a potential path to headless and browser-accessible deployments.

However, server-mode compilation does not automatically provide production-grade authentication, multiuser isolation, authorization, or distributed persistence. Those capabilities must be introduced and validated when needed.

The initial target platform will be Windows, with macOS and Linux support evaluated subsequently.

### 2. Processing orchestration: Bacalhau

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

Ordinary application operations do not need to become Bacalhau jobs. Long-running evidence-processing workloads should have a single lifecycle owner.

### 3. Execution environments

The initial architecture will recognize two Bacalhau execution mechanisms:

**WebAssembly**

- Suitable for lightweight, portable, compatible workloads.
- Does not require Docker.
- Limited by the capabilities of Bacalhau's supported WASI runtime.
- Requires actual workload compatibility testing.

**Docker**

- Suitable for existing forensic tools and specialized processing environments.
- Enables reuse of containerized OCR, media-processing, parsing, and indexing tools.
- Requires a functioning Docker-compatible environment on the compute node.
- May introduce installation, licensing, security, and maintenance requirements.

Docker is a capability dependency, not a commercial feature gate.

Users remain free to configure supported execution environments independently.

### 4. Local-first topology

The initial Bacalhau installation will use a local hybrid node, combining orchestration and computation.

The proposed deployment consists of:

- The VIII desktop application.
- A locally managed Bacalhau process.
- The required processing modules or container images.
- Local evidence and application storage.

Adding remote compute nodes should not require rewriting the application's job contracts.

The installation experience must hide orchestration details from nontechnical users.

### 5. Evidence and execution boundaries

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

## Proposed architecture

**Presentation**

- Wails v3 desktop application.
- Shared web frontend where technically appropriate.

**Application**

- Go domain and application services.
- Evidence and provenance model.
- Processing API and execution policies.
- Bacalhau integration adapter.

**Execution**

- Bacalhau local or remote orchestrator.
- Authorized compute nodes.
- WASM workers.
- Optional Docker workers.

**Storage**

- Local-first evidence storage.
- Application metadata persistence.
- Explicitly managed derived artifacts.

The exact local database and future distributed storage strategy are outside the scope of this ADR.

The architecture must not assume that adding a compute node automatically makes evidence accessible to that node.

## Update and maintenance strategy

The MVP must support controlled updates when no evidence-processing jobs are active.

The intended update workflow is:

1. Enter maintenance mode and reject new processing requests.
2. Confirm that no active or pending jobs would be disrupted.
3. Preserve application state and required rollback information.
4. Download and verify versioned updates.
5. Update the application, Bacalhau runtime, and workers as applicable.
6. Restart affected processes.
7. Run automated health checks and synthetic processing tests.
8. Resume normal operation only after successful validation.

Wails v3 provides application self-update capabilities, but this does not automatically cover Bacalhau, Docker, worker images, or application data migrations.

Component update coordination remains an VIII responsibility.

Hot code swapping is not a requirement. Controlled restart during an idle maintenance window is acceptable.

## Alternatives considered

### Elixir + Phoenix LiveView + Oban

**Advantages**

- Mature concurrency and supervision model.
- Durable job execution through Oban.
- Strong support for web applications and distributed services.
- Existing engineering familiarity.

**Trade-offs**

- A custom execution and deployment architecture may still be required for distributed, data-local workloads.
- Combining Oban and Bacalhau risks overlapping orchestration responsibilities.
- Desktop distribution requires additional packaging decisions.

**Assessment:** viable, but not preferred for the proposed MVP architecture. The decision does not imply that Elixir is inherently unsuitable for low-ops systems.

### Flutter + Go

**Advantages**

- Rich desktop and mobile UI capabilities.
- Cross-platform interface development.

**Trade-offs**

- Introduces Dart alongside Go.
- Requires integration between frontend and backend execution environments.
- Adds tooling and packaging complexity without a demonstrated advantage for the initial evidence-review workflow.

**Assessment:** not selected for the MVP. Reconsider if native UI requirements or mobile deployment become central.

### Go + browser-based interface without Wails

**Advantages**

- Potentially smaller initial technology surface.
- Natural deployment as a web service.
- No desktop framework dependency.

**Trade-offs**

- Desktop installation, local application lifecycle, OS integration, and updates require additional design.
- A browser-only experience may be less suitable for the intended workstation product.

**Assessment:** retained as a fallback if Wails packaging or runtime behavior creates unacceptable overhead.

### Go + custom job scheduler

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

## Consequences

### Positive

- Go becomes the primary backend language.
- Wails provides an integrated desktop application model.
- Bacalhau offers a potential evolution from local to distributed execution.
- A single orchestration system reduces duplicated responsibilities.
- Existing containerized forensic tools can be reused.
- The application can expose the same processing concepts across deployment modes.
- Managed installation and maintenance become well-defined optional service opportunities.

### Negative

- Bacalhau remains a separately managed process.
- A single application executable does not mean a single-component deployment.
- The Docker dependency may complicate Windows installations.
- WASM compatibility limitations may restrict Docker-free workflows.
- Wails v3 is relatively new and requires platform-specific validation.
- Multiple application and worker versions introduce compatibility-management responsibilities.
- Distributed execution introduces data-placement, security, and networking complexity.

## Validation requirements

This decision remains **Proposed** until a proof of concept demonstrates acceptable operational characteristics.

The proof of concept must validate:

### A. Desktop installation

- Build and package VIII for Windows.
- Start the application without development tools installed.
- Start or connect to the local Bacalhau process.
- Present meaningful diagnostics when required dependencies are missing.
- Complete installation and first use without requiring command-line interaction from the end user.

### B. Local forensic workflow

- Import a small, synthetic UFDR XML fixture.
- Execute a compatible processing worker through Bacalhau.
- Normalize supported messages.
- Present searchable results with references to source records.
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

- Update the application during an idle period.
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

Compare against a minimal Go application using local native execution, without implementing a full competing scheduler.

## Acceptance criteria

The decision may be accepted if:

1. A nontechnical user can install and execute the basic workflow without manually configuring orchestration infrastructure.
2. The same processing contract supports local and authorized remote execution.
3. Bacalhau provides reliable job lifecycle management without substantial custom scheduling code.
4. The application can be safely updated during idle periods.
5. Operational complexity remains acceptable for a single workstation.
6. Source-linked results remain verifiable independently of the execution location.
7. Docker is not required for any workflow explicitly advertised as Docker-free.

No performance, deployment, or reliability claims will be considered validated without reproducible tests.

## Reconsideration triggers

Revisit this ADR if:

- Bacalhau requires a substantial custom executor for priority workflows.
- Docker-free processing introduces disproportionate engineering cost.
- Local execution requires more operational components than an equivalent native solution.
- Wails cannot provide acceptable Windows installation and update behavior.
- Server-mode deployment requires major duplication of application logic.
- Bacalhau's distribution model cannot satisfy the project's data-locality and authorization requirements.
- Another architecture demonstrates materially lower lifecycle cost while preserving the roadmap.

## Out of scope

This ADR does not decide:

- The complete frontend component framework.
- The final database or search engine.
- The cloud infrastructure provider.
- Multi-tenant architecture.
- The complete forensic tool catalog.
- AI model providers.
- Commercial pricing or support contracts.
- The final packaging strategy for Docker Desktop.
- Whether a future native execution adapter will be justified.

These decisions should follow specific product or technical requirements.

## References

- [VIII — Digital Evidence Workbench](https://github.com/vmenezesdev/viii-digital-evidence-workbench)
- [Wails v3 documentation](https://v3.wails.io/)
- [Wails server build](https://v3.wails.io/guides/server-build/)
- [Wails updater](https://wails.io/guides/updater/)
- [Bacalhau architecture](https://bacalhau.org/docs/overview/architecture/)
- [Bacalhau execution engines](https://bacalhau.org/docs/references/faq/)
- [Docker Desktop licensing](https://docs.docker.com/subscription-billing/desktop-license/)

## Decision summary

Adopt **Go + Wails v3 + Bacalhau** as the proposed MVP architecture, subject to proof-of-concept validation.

Prefer a single processing orchestrator, local-first installation, optional Docker execution, and stable job contracts across deployment environments.

The decision optimizes for **minimum lifecycle complexity across the local-to-distributed roadmap**, rather than minimum implementation effort for the first isolated feature.