# PDF workflow: documentation

Local import, search and cited answers for PDFs of up to 30,000 pages, on Bacalhau +
WASI with optional Docker.

| Read | For |
| --- | --- |
| [chunking.md](chunking.md) | **Start here.** Problem, architecture, measured results, limits and reproduction. |
| [acceptance.md](acceptance.md) | Audit of every requirement in the brief against its evidence. |
| [MVP-PDF.md](MVP-PDF.md) | The original product brief and acceptance criteria. |
| [product.md](product.md) | Build, package and run; repeat commands; history of the in-place MVP. |
| [experiment-cli.md](experiment-cli.md) | Detached start/pause/resume/status CLI for long runs. |
| [journal.md](journal.md) | Technical journal of the in-place MVP, including failed attempts. |
| [ADR-0003](../../adr/0003-chunk-large-pdfs-with-flpdf-wasi.md) | The chunking decision. |

Evidence: [`results/chunked/`](results/chunked/README.md) (current, chunked processing) and
[`results/mvp/`](results/mvp/README.md) (in-place MVP). The chunker experiments that led to
ADR-0003 live in [`experiments/pdf-chunker/`](../../../experiments/pdf-chunker/README.md).
Code: `cmd/desktop/pdf_*.go`, `workers/pdf` (OCR worker), `workers/pdf-chunker` (chunker),
`scripts/*pdf*` (packaging, validation and benchmarks).
