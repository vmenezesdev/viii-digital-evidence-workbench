FROM debian:bookworm-slim AS build
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates curl cmake ninja-build make python3 xz-utils && rm -rf /var/lib/apt/lists/*
WORKDIR /build
RUN curl -fL https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-25/wasi-sdk-25.0-x86_64-linux.tar.gz | tar xz && mv wasi-sdk-25.0-x86_64-linux /opt/wasi-sdk
RUN curl -fL https://github.com/DanBloomberg/leptonica/releases/download/1.85.0/leptonica-1.85.0.tar.gz | tar xz
RUN cmake -S leptonica-1.85.0 -B lept -G Ninja \
    -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake -DCMAKE_INSTALL_PREFIX=/opt/lept \
    -DCMAKE_BUILD_TYPE=Release -DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY \
    -DBUILD_SHARED_LIBS=OFF -DBUILD_PROG=OFF -DENABLE_ZLIB=OFF -DENABLE_PNG=OFF -DENABLE_JPEG=OFF \
    -DENABLE_TIFF=OFF -DENABLE_WEBP=OFF -DENABLE_OPENJPEG=OFF -DENABLE_GIF=OFF \
    -DCMAKE_C_FLAGS='-D_WASI_EMULATED_PROCESS_CLOCKS -D_WASI_EMULATED_SIGNAL -Wno-implicit-function-declaration' \
    && cmake --build lept -j4 && cmake --install lept
RUN curl -fL https://github.com/tesseract-ocr/tesseract/archive/refs/tags/5.5.0.tar.gz | tar xz
COPY scripts/pdf-wasi-threads.h tesseract-5.5.0/src/pdf-wasi-threads.h
COPY scripts/patch-pdf-tesseract.py /build/patch-pdf-tesseract.py
RUN python3 patch-pdf-tesseract.py tesseract-5.5.0
RUN cmake -S tesseract-5.5.0 -B tess -G Ninja \
    -DCMAKE_TOOLCHAIN_FILE=/opt/wasi-sdk/share/cmake/wasi-sdk.cmake -DCMAKE_INSTALL_PREFIX=/opt/tess \
    -DCMAKE_PREFIX_PATH=/opt/lept -DCMAKE_BUILD_TYPE=Release -DCMAKE_TRY_COMPILE_TARGET_TYPE=STATIC_LIBRARY \
    -DLEPT_TIFF_RESULT=1 -DLEPT_TIFF_RESULT__TRYRUN_OUTPUT= \
    -DBUILD_SHARED_LIBS=OFF -DBUILD_TRAINING_TOOLS=OFF -DBUILD_TESTS=OFF -DBUILD_TESSERACT_BINARY=OFF \
    -DDISABLED_LEGACY_ENGINE=ON -DDISABLE_ARCHIVE=ON -DDISABLE_CURL=ON -DGRAPHICS_DISABLED=ON -DOPENMP_BUILD=OFF \
    -DCMAKE_CXX_FLAGS='-msimd128 -fno-exceptions -D_WASI_EMULATED_PROCESS_CLOCKS -D_WASI_EMULATED_SIGNAL' \
    && cmake --build tess --target libtesseract -j4
COPY scripts/pdf-ocr-proof.cpp /build/ocr-proof.cpp
RUN mkdir /out && /opt/wasi-sdk/bin/clang++ -O2 -msimd128 -fno-exceptions \
    -Itesseract-5.5.0/include -Itess/include -I/opt/lept/include ocr-proof.cpp \
    /build/tess/libtesseract.a /opt/lept/lib/libleptonica.a \
    -lwasi-emulated-process-clocks -lwasi-emulated-signal \
    -Wl,-z,stack-size=16777216 -Wl,--max-memory=1073741824 -o /out/ocr-proof.wasm
FROM scratch AS artifacts
COPY --from=build /out/ /
COPY --from=build /build/tess/libtesseract.a /lib/
COPY --from=build /opt/lept/lib/libleptonica.a /lib/
COPY --from=build /opt/wasi-sdk/share/wasi-sysroot/lib/wasm32-wasi/libc++.a /lib/
COPY --from=build /opt/wasi-sdk/share/wasi-sysroot/lib/wasm32-wasi/libc++abi.a /lib/
COPY --from=build /opt/wasi-sdk/share/wasi-sysroot/lib/wasm32-wasi/libwasi-emulated-process-clocks.a /lib/
COPY --from=build /opt/wasi-sdk/share/wasi-sysroot/lib/wasm32-wasi/libwasi-emulated-signal.a /lib/
