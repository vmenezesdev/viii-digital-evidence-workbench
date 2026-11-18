"""Generate reproducible mixed synthetic PDFs; never reads real evidence."""
import argparse
import hashlib
import io
import json
from pathlib import Path
import random
import zlib

from PIL import Image, ImageDraw, ImageFont


def pdf_text(text):
    return b"(" + text.encode("cp1252").replace(b"\\", b"\\\\").replace(b"(", b"\\(").replace(b")", b"\\)") + b")"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("output", type=Path)
    parser.add_argument("--pages", type=int, default=12)
    parser.add_argument("--ocr-every", type=int, default=4)
    args = parser.parse_args()
    if not 2 <= args.pages <= 30000 or args.ocr_every < 2:
        parser.error("use 2..30000 pages and --ocr-every >= 2")
    if args.output.exists():
        parser.error("output already exists; choose a new path")
    args.output.parent.mkdir(parents=True, exist_ok=True)
    target = args.pages - 1
    scanned = [n for n in range(1, args.pages + 1) if n % args.ocr_every == 0 or n == target]
    scanned_set = set(scanned)
    font = ImageFont.truetype("C:/Windows/Fonts/arial.ttf", 25)
    title = ImageFont.truetype("C:/Windows/Fonts/arial.ttf", 36)
    paragraph = "A prova documental confirma a obrigação de pagamento e o prazo contratual."
    # Stream trusted synthetic objects; keep only xref offsets, not 30k pages in RAM.
    with args.output.open("xb") as pdf:
        pdf.write(b"%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
        offsets = {}
        kids = []
        next_id = 4

        def emit(number, body):
            offsets[number] = pdf.tell()
            pdf.write(f"{number} 0 obj\n".encode() + body + b"\nendobj\n")

        for number in range(1, args.pages + 1):
            page_id, content_id = next_id, next_id + 1
            next_id += 2
            kids.append(f"{page_id} 0 R")
            resources = b"/Font << /F1 3 0 R >>"
            if number in scanned_set:
                image_id = next_id
                next_id += 1
                resources += f" /XObject << /Im0 {image_id} 0 R >>".encode()
                image = scan_image(number, target, font, title, paragraph)
                buffer = io.BytesIO()
                image.save(buffer, "JPEG", quality=85)
                jpeg = buffer.getvalue()
                emit(image_id, f"<< /Type /XObject /Subtype /Image /Width 1240 /Height 1754 /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode /Length {len(jpeg)} >>\nstream\n".encode() + jpeg + b"\nendstream")
                content = b"q 595 0 0 842 0 0 cm /Im0 Do Q"
            else:
                content = b"BT /F1 18 Tf 44 790 Td " + pdf_text(f"Documento sintético - página física {number}") + b" Tj ET\n"
                for line in range(35):
                    content += f"BT /F1 11 Tf 1 0 0 1 44 {740-line*18} Tm ".encode() + pdf_text(f"{line+1:02}. {paragraph}") + b" Tj ET\n"
                content += b"BT /F1 11 Tf 44 60 Td " + pdf_text(f"Contrato {number:08d}. Referência digital ARARA.") + b" Tj ET"
            content = zlib.compress(content)
            emit(content_id, f"<< /Filter /FlateDecode /Length {len(content)} >>\nstream\n".encode() + content + b"\nendstream")
            emit(page_id, f"<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents {content_id} 0 R /Resources << ".encode() + resources + b" >> >>")
            if number % 1000 == 0:
                print(json.dumps(dict(generated=number, bytes=pdf.tell())), flush=True)
        emit(1, b"<< /Type /Catalog /Pages 2 0 R >>")
        emit(2, f"<< /Type /Pages /Count {args.pages} /Kids [".encode() + " ".join(kids).encode() + b"] >>")
        emit(3, b"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
        xref = pdf.tell()
        pdf.write(f"xref\n0 {next_id}\n0000000000 65535 f \n".encode())
        for number in range(1, next_id):
            pdf.write(f"{offsets[number]:010d} 00000 n \n".encode())
        pdf.write(f"trailer\n<< /Size {next_id} /Root 1 0 R >>\nstartxref\n{xref}\n%%EOF\n".encode())
    with args.output.open("rb") as source:
        digest = hashlib.file_digest(source, "sha256").hexdigest()
    manifest = dict(generator="stream-v1", pages=args.pages, scanned=scanned, ocr_ratio=len(scanned) / args.pages,
                    target_page=target, target_term="JABUTICABA", blank_page=8 if 8 in scanned else None,
                    bytes=args.output.stat().st_size, sha256=digest)
    args.output.with_suffix(".json").write_text(json.dumps(manifest, indent=2), encoding="utf-8")
    assert target in scanned and manifest["bytes"] > 0
    print(json.dumps(manifest))


def scan_image(number, target, font, title, paragraph):
    image = Image.new("RGB", (1240, 1754), "white")
    draw = ImageDraw.Draw(image)
    if number != 8:
        draw.text((90, 90), f"Documento sintético - página física {number}", font=title, fill="black")
        for line in range(30):
            draw.text((90, 190 + line * 43), f"{line + 1:02}. {paragraph}", font=font, fill="black")
        if number == target:
            draw.text((90, 1580), "JABUTICABA: pagamento previsto para 15 de março.", font=font, fill="black")
        rng = random.Random(number)
        for _ in range(3000):
            x, y = rng.randrange(1240), rng.randrange(1754)
            draw.point((x, y), fill=(190, 190, 190))
    return image


if __name__ == "__main__":
    main()
