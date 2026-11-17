# scratch is Docker's reserved empty base. It has no upstream layers.
FROM scratch
COPY --chmod=0555 bench-worker-linux /bench-worker
USER 65532:65532
ENTRYPOINT ["/bench-worker"]
