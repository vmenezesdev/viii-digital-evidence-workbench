fn main() {
    println!("cargo:rerun-if-env-changed=VIII_PDF_WASI_LIBS");
    if std::env::var("CARGO_CFG_TARGET_OS").as_deref() == Ok("wasi") {
        let dir = std::env::var("VIII_PDF_WASI_LIBS").expect(
            "build scripts/pdf-worker.Dockerfile first; set VIII_PDF_WASI_LIBS to artifacts/lib",
        );
        println!("cargo:rustc-link-search=native={dir}");
        for name in [
            "tesseract",
            "leptonica",
            "c++",
            "c++abi",
            "wasi-emulated-process-clocks",
            "wasi-emulated-signal",
        ] {
            println!("cargo:rustc-link-lib=static={name}");
        }
    } else {
        println!("cargo:rustc-link-lib=tesseract");
    }
}
