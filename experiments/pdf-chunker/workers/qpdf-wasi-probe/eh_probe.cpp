// Minimal gate for porting qpdf (C++, exception-heavy) to WASI Preview 1.
// qpdf reports almost every parse error with C++ exceptions, so a runtime that
// cannot execute the Wasm exception-handling proposal cannot run qpdf at all.
#include <cstdio>
#include <stdexcept>
#include <string>

static int parse(int n) {
    if (n > 2) throw std::runtime_error("synthetic parse error " + std::to_string(n));
    return n;
}

int main(int argc, char **) {
    try {
        parse(argc + 2);
    } catch (const std::exception &e) {
        std::printf("caught: %s\n", e.what());
        return 0;
    }
    std::puts("exception was not raised");
    return 2;
}
