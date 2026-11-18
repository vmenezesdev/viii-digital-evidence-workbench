FROM rust:1.99.1-bookworm AS build
RUN apt-get update && apt-get install -y --no-install-recommends libtesseract-dev && rm -rf /var/lib/apt/lists/*
WORKDIR /build
COPY workers/pdf/ .
RUN cargo test --release --locked && cargo build --release --locked

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends libtesseract5 && rm -rf /var/lib/apt/lists/*
COPY --from=build /build/target/release/viii-pdf-worker /pdf-worker
ENTRYPOINT ["/pdf-worker"]
