#include <tesseract/baseapi.h>
#include <leptonica/allheaders.h>
#include <cstdio>
#include <string>
#include <cerrno>
#include <cstdlib>
#include <unistd.h>

#ifdef __wasi__
// Leptonica's optional external viewers/temp helpers are unavailable in WASI.
extern "C" int system(const char*) { errno = ENOSYS; return -1; }
extern "C" int mkstemp(char*) { errno = ENOSYS; return -1; }
#endif

static std::string json(const char* value) {
  std::string out = "\"";
  for (const unsigned char* p = (const unsigned char*)value; *p; ++p) {
    if (*p == '"' || *p == '\\') out += '\\';
    if (*p < 32) {
      char escaped[7];
      std::snprintf(escaped, sizeof escaped, "\\u%04x", *p);
      out += escaped;
    } else out += *p;
  }
  return out + '"';
}

int main(int argc, char** argv) {
  if (argc != 5) return 2;
  tesseract::TessBaseAPI api;
  if (api.Init(argv[2], "por", tesseract::OEM_LSTM_ONLY)) return 3;
  Pix* image = pixRead(argv[1]);
  if (!image) return 4;
  api.SetPageSegMode(tesseract::PSM_SINGLE_BLOCK);
  api.SetImage(image);
  api.SetSourceResolution(150);
  char* text = api.GetUTF8Text();
  if (!text) { pixDestroy(&image); return 5; }
  FILE* output = std::fopen(argv[3], "wb");
  if (!output) { delete[] text; pixDestroy(&image); return 6; }
  const std::string record = "{\"page\":1,\"source\":\"ocr\",\"status\":\"complete\",\"tool\":\"tesseract\",\"version\":" +
      json(api.Version()) + ",\"text\":" + json(text) + "}\n";
  bool failed = std::fwrite(record.data(), 1, record.size(), output) != record.size();
  failed = std::fclose(output) != 0 || failed;
  delete[] text;
  pixDestroy(&image);
  if (failed) return 7;
  // The noop publisher deletes outputs on exit; wait for durable host ingestion.
  for (int i = 0; i < 1200; ++i) {
    FILE* accepted = std::fopen(argv[4], "rb");
    if (accepted) {
      std::fclose(accepted);
      api.End();
      std::puts("OCR artifact accepted");
      std::fflush(stdout);
      // Bacalhau v1.9.5 treats a returning WASI _start as exit -1.
      std::_Exit(0);
    }
    usleep(100000);
  }
  return 8;
}
