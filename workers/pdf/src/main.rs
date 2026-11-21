use hayro::hayro_interpret::{
    self as interpret, BlendMode, ClipPath, Context, Device, GlyphDrawMode, Image,
    InterpreterCache, InterpreterSettings, Paint, PathDrawMode, SoftMask, font::Glyph,
    hayro_cmap::BfString,
};
use hayro::hayro_syntax::Pdf;
use hayro::{RenderCache, RenderSettings};
use kurbo::{Affine, BezPath, Rect};
use serde_json::json;
use std::{
    fs,
    io::{self, Read},
    path::Path,
    thread,
    time::{Duration, Instant},
};
mod ocr;

#[derive(Default)]
struct Text {
    value: String,
    last: Option<(f64, f64)>,
}

impl Device<'_> for Text {
    fn set_soft_mask(&mut self, _: Option<SoftMask<'_>>) {}
    fn set_blend_mode(&mut self, _: BlendMode) {}
    fn draw_path(&mut self, _: &BezPath, _: Affine, _: &Paint<'_>, _: &PathDrawMode) {}
    fn push_clip_path(&mut self, _: &ClipPath) {}
    fn push_transparency_group(&mut self, _: f32, _: Option<SoftMask<'_>>, _: BlendMode) {}
    fn draw_image(&mut self, _: Image<'_, '_>, _: Affine) {}
    fn pop_clip_path(&mut self) {}
    fn pop_transparency_group(&mut self) {}
    fn draw_glyph(
        &mut self,
        glyph: &Glyph<'_>,
        transform: Affine,
        local: Affine,
        _: &Paint<'_>,
        _: &GlyphDrawMode,
    ) {
        let position = (transform * local).translation();
        let here = (position.x, position.y);
        if let Some(previous) = self.last {
            if previous == here {
                return;
            }
            if (previous.1 - here.1).abs() > 2.0 {
                self.value.push('\n');
            }
        }
        match glyph.as_unicode() {
            Some(BfString::Char(c)) => self.value.push(c),
            Some(BfString::String(s)) => self.value.push_str(&s),
            None => {}
        }
        self.last = Some(here);
    }
}

fn selected(first: usize, last: usize, total: usize) -> Result<(), &'static str> {
    if first == 0 || last < first || last > total || total > 30000 {
        Err("invalid physical PDF page range (maximum 30000 pages)")
    } else {
        Ok(())
    }
}

fn read_bounded(reader: &mut impl Read, expected: usize) -> io::Result<Vec<u8>> {
    let mut bytes = Vec::new();
    bytes
        .try_reserve_exact(expected)
        .map_err(io::Error::other)?;
    // Large Windows ReadFile buffers can fail to be page-locked under pressure.
    let mut block = [0u8; 64 * 1024];
    loop {
        let n = match reader.read(&mut block) {
            Err(e) if e.kind() == io::ErrorKind::Interrupted => continue,
            result => result?,
        };
        if n == 0 {
            break;
        }
        if bytes.len() + n > expected {
            return Err(io::Error::other("original PDF length changed during read"));
        }
        bytes.extend_from_slice(&block[..n]);
    }
    if bytes.len() != expected {
        return Err(io::Error::new(
            io::ErrorKind::UnexpectedEof,
            "original PDF read was incomplete",
        ));
    }
    Ok(bytes)
}

fn run() -> Result<(), Box<dyn std::error::Error>> {
    let args: Vec<String> = std::env::args().collect();
    if args.len() < 7 || args.len() > 8 {
        return Err("usage: worker original.pdf first last output-dir ack-path render|extract|ocr [model-dir]".into());
    }
    let first: usize = args[2].parse()?;
    let requested_last: usize = args[3].parse()?;
    let out = Path::new(&args[4]);
    let mode = args[6].as_str();
    if !["render", "extract", "ocr"].contains(&mode) {
        return Err("invalid mode".into());
    }
    let mut ocr = None;
    // shortcut: hayro owns the PDF byte buffer; streaming is needed near WASI's memory limit.
    let bytes = {
        let mut file = fs::File::open(&args[1]).map_err(|e| format!("open original: {e:?}"))?;
        let size = usize::try_from(
            file.metadata()
                .map_err(|e| format!("stat original: {e:?}"))?
                .len(),
        )?;
        read_bounded(&mut file, size)
            .map_err(|e| format!("read original with 64 KiB requests: {e:?}"))?
    };
    let input_bytes = bytes.len();
    let pdf = Pdf::new(bytes).map_err(|e| format!("PDF parse failed: {e:?}"))?;
    let last = if requested_last == 0 {
        pdf.pages().len()
    } else {
        requested_last
    };
    selected(first, last, pdf.pages().len())?;
    let skip_path = Path::new(&args[5])
        .parent()
        .ok_or("invalid acknowledgement path")?
        .join("skip.json");
    let skip: Vec<usize> = if skip_path.exists() {
        serde_json::from_slice(
            &fs::read(skip_path).map_err(|e| format!("read skip control: {e:?}"))?,
        )?
    } else {
        Vec::new()
    };
    if skip.iter().any(|p| *p < first || *p > last) {
        return Err("skip page outside assigned range".into());
    }
    fs::write(out.join("info.json"), json!({"pages":pdf.pages().len(),"bytes":input_bytes,"first":first,"last":last,"tool":"hayro","version":"0.7.1"}).to_string()).map_err(|e| format!("write PDF metadata: {e:?}"))?;
    let settings = InterpreterSettings::default();
    for number in first..=last {
        if skip.contains(&number) {
            continue;
        }
        let page = &pdf.pages()[number - 1];
        let (width, height) = page.render_dimensions();
        if width <= 0.0 || height <= 0.0 || width > 2000.0 || height > 2000.0 {
            fs::write(out.join(format!("page-{number:06}.json")), json!({"page":number,"text":"","source":"digital","status":"error","tool":"hayro","version":"0.7.1","error":"Page dimensions exceed render memory budget"}).to_string())?;
            continue;
        }
        let cache = InterpreterCache::new();
        let mut context = Context::new(
            Affine::IDENTITY,
            Rect::new(0.0, 0.0, width as f64, height as f64),
            &cache,
            page.xref(),
            settings.clone(),
        );
        let mut text = Text::default();
        interpret::interpret_page(page, &mut context, &mut text);
        let mut status = if text.value.trim().chars().count() >= 20 {
            "complete"
        } else {
            "needs_ocr"
        };
        let (mut source, mut tool) = ("digital", "hayro");
        let mut version = String::from("0.7.1");
        if mode == "render" || (mode == "ocr" && status == "needs_ocr") {
            // A fresh cache per page releases decoded images and paths before the next page.
            let cache = RenderCache::new();
            let pixmap = hayro::render(
                page,
                &cache,
                &settings,
                &RenderSettings {
                    x_scale: 2.0,
                    y_scale: 2.0,
                    bg_color: hayro::vello_cpu::color::palette::css::WHITE,
                    ..Default::default()
                },
            );
            if mode == "ocr" {
                if ocr.is_none() {
                    ocr = Some(ocr::Ocr::new(
                        args.get(7).ok_or("OCR model directory missing")?,
                    )?);
                }
                text.value = ocr.as_mut().unwrap().read(&pixmap)?;
                source = "ocr";
                tool = "tesseract";
                version = ocr::version();
                status = if text.value.trim().is_empty() {
                    "unreadable"
                } else {
                    "complete"
                };
            } else {
                fs::write(
                    out.join(format!("page-{number:06}.png")),
                    pixmap.into_png()?,
                )?;
            }
        }
        fs::write(out.join(format!("page-{number:06}.json")), json!({"page":number,"text":text.value,"source":source,"status":status,"tool":tool,"version":version}).to_string()).map_err(|e| format!("write physical page {number}: {e:?}"))?;
    }
    fs::write(out.join("ready"), b"ready").map_err(|e| format!("write ready marker: {e:?}"))?;
    let deadline = Instant::now() + Duration::from_secs(120);
    while !Path::new(&args[5]).exists() {
        if Instant::now() >= deadline {
            return Err("host ingestion acknowledgement timed out".into());
        }
        thread::sleep(Duration::from_millis(100));
    }
    Ok(())
}

fn main() {
    match run() {
        Ok(()) => std::process::exit(0),
        Err(error) => {
            eprintln!("{error}");
            std::process::exit(1);
        }
    }
}

#[cfg(test)]
mod tests {
    use std::io::{self, Cursor, Read};

    #[test]
    fn bounded_file_reads_preserve_bytes_and_reject_incomplete_input() {
        struct Narrow(Cursor<Vec<u8>>);
        impl Read for Narrow {
            fn read(&mut self, buf: &mut [u8]) -> io::Result<usize> {
                assert!(buf.len() <= 64 * 1024, "oversized host I/O request");
                let len = buf.len().min(12345);
                self.0.read(&mut buf[..len])
            }
        }
        let input: Vec<_> = (0..200_013).map(|i| (i % 251) as u8).collect();
        assert_eq!(
            super::read_bounded(&mut Narrow(Cursor::new(input.clone())), input.len()).unwrap(),
            input
        );
        assert_eq!(
            super::read_bounded(&mut Cursor::new(vec![1, 2, 3]), 4)
                .unwrap_err()
                .kind(),
            io::ErrorKind::UnexpectedEof
        );
        assert!(super::read_bounded(&mut Cursor::new(vec![1, 2, 3]), 2).is_err());
    }

    #[test]
    fn physical_page_range() {
        assert!(super::selected(1, 30000, 30000).is_ok());
        for (first, last, total) in [(0, 2, 3), (3, 2, 3), (1, 4, 3), (1, 1, 30001)] {
            assert!(super::selected(first, last, total).is_err());
        }
    }
}
