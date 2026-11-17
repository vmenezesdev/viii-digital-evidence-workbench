# ADR-0002: Adopt Go and Wails v3 for the VIII application layer

- **Status:** Proposed
- **Date:** 2026-11-16
- **Decision scope:** VIII-owned application services and desktop framework
- **Project:** VIII — Digital Evidence Workbench
- **Origin:** Application-stack decision extracted from ADR-0001; the selected direction is unchanged and platform validation remains pending.

## Context

VIII needs application services for cases, evidence identity, processing requests, source-linked findings and human review, with a desktop interface accessible to nontechnical users. The initial target is Windows; macOS and Linux support will be evaluated subsequently.

The application stack and the job runtime solve different problems. [ADR-0001](0001-adopt-go-wails-v3-bacalhau.md) selects Bacalhau for job orchestration. That runtime choice neither requires Go nor selects a desktop framework.

## Decision

Use **Go for VIII-owned application services and Wails v3 for desktop packaging and interaction with a web-based frontend**.

Use a lightweight HTML/CSS/TypeScript frontend; select its component framework independently. Go services own application/domain logic, evidence management, persistence integration and the adapter to the job runtime. Application code may perform ordinary validation, hashing, search and presentation operations; processing-job execution follows ADR-0001.

Wails provides desktop capabilities without a separate application backend runtime. Its server build mode is a potential path to browser-accessible or headless deployments, subject to separate validation. Server compilation alone does not provide authentication, multiuser isolation, authorization or distributed persistence.

## Alternatives considered

| Option | Benefit | Reason for retaining Go + Wails |
| --- | --- | --- |
| Elixir + Phoenix LiveView | Concurrency, supervision, web application support and existing engineering familiarity. | Desktop packaging would require additional choices. This is a viable alternative, but not the selected application stack. Job orchestration is assessed separately in ADR-0001. |
| Flutter + Go | Rich desktop and mobile UI capabilities. | Adds Dart and frontend/backend integration without a demonstrated advantage for the initial review workflow. Reconsider if native UI or mobile requirements become central. |
| Go + browser interface | Smaller desktop-framework surface and a natural web-service path. | Local application lifecycle, OS integration, installation and updates still need a design. Retain as an alternative if Wails packaging or runtime behavior is unacceptable. |

## Consequences

- Go is the primary application backend language; a web frontend runs inside the Wails desktop shell.
- Desktop OS integration and distribution use Wails, with platform-specific verification required.
- The application and its WebView dependency need installation diagnostics and a supported Windows matrix.
- Application updates do not automatically update Bacalhau, workers, Docker or data schemas; coordination across those boundaries remains explicit.
- Changing the application stack does not by itself change the selected job runtime, evidence authorization or provenance requirements.

## Validation and reconsideration

Validate Windows packaging and first launch on a clean machine without development tools, WebView prerequisites and diagnostics, the import/search/source-inspection user journey, restart behavior and application update/rollback with state preservation. Record the exact application build and platform versions. Builds and handler tests alone do not establish a usable desktop installation.

Reconsider this decision if Wails cannot provide acceptable desktop installation, OS integration, runtime behavior or updates, or if a required server deployment would substantially duplicate application logic. These are application-stack questions; they do not automatically invalidate Bacalhau's orchestration role.

## Out of scope

- Job orchestration and the WASI/Docker execution boundary, defined by ADR-0001.
- Worker implementation languages and the forensic tool catalog.
- The final frontend framework, database and search engine.
- Production server authentication, multitenancy and deployment topology.
- The detailed application installer/updater implementation.

## References

- [ADR-0001: Bacalhau runtime as job orchestrator](0001-adopt-go-wails-v3-bacalhau.md)
- [Windows PoC build and UI verification evidence](../poc/pre-merge-review.md)
- [Post-merge validation status in issue #9](https://github.com/vmenezesdev/viii-digital-evidence-workbench/issues/9)
- [Wails v3 documentation](https://v3.wails.io/)
- [Wails server build](https://v3.wails.io/guides/server-build/)
- [Wails updater](https://wails.io/guides/updater/)
