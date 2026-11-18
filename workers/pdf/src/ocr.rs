use std::ffi::{CStr, CString, c_char, c_int, c_void};

unsafe extern "C" {
    fn TessBaseAPICreate() -> *mut c_void;
    fn TessBaseAPIDelete(api: *mut c_void);
    fn TessBaseAPIInit3(api: *mut c_void, data: *const c_char, language: *const c_char) -> c_int;
    fn TessBaseAPISetPageSegMode(api: *mut c_void, mode: c_int);
    fn TessBaseAPISetImage(
        api: *mut c_void,
        image: *const u8,
        width: c_int,
        height: c_int,
        bytes_per_pixel: c_int,
        bytes_per_line: c_int,
    );
    fn TessBaseAPISetSourceResolution(api: *mut c_void, dpi: c_int);
    fn TessBaseAPIGetUTF8Text(api: *mut c_void) -> *mut c_char;
    fn TessDeleteText(text: *mut c_char);
    fn TessVersion() -> *const c_char;
}

pub fn version() -> String {
    unsafe { CStr::from_ptr(TessVersion()).to_string_lossy().into_owned() }
}

pub struct Ocr(*mut c_void);
impl Ocr {
    pub fn new(model: &str) -> Result<Self, Box<dyn std::error::Error>> {
        let model = CString::new(model)?;
        let api = unsafe { TessBaseAPICreate() };
        if api.is_null() {
            return Err("OCR allocation failed".into());
        }
        let result = Self(api);
        if unsafe { TessBaseAPIInit3(api, model.as_ptr(), c"por".as_ptr()) } != 0 {
            return Err("Portuguese OCR model initialization failed".into());
        }
        unsafe {
            TessBaseAPISetPageSegMode(api, 3);
        }
        Ok(result)
    }
    pub fn read(&mut self, pixmap: &hayro::vello_cpu::Pixmap) -> Result<String, &'static str> {
        unsafe {
            TessBaseAPISetImage(
                self.0,
                pixmap.data_as_u8_slice().as_ptr(),
                pixmap.width() as i32,
                pixmap.height() as i32,
                4,
                pixmap.width() as i32 * 4,
            );
            TessBaseAPISetSourceResolution(self.0, 144);
            let text = TessBaseAPIGetUTF8Text(self.0);
            if text.is_null() {
                return Err("OCR recognition failed");
            }
            let value = CStr::from_ptr(text).to_string_lossy().into_owned();
            TessDeleteText(text);
            Ok(value)
        }
    }
}
impl Drop for Ocr {
    fn drop(&mut self) {
        unsafe {
            TessBaseAPIDelete(self.0);
        }
    }
}

#[cfg(target_os = "wasi")]
#[unsafe(no_mangle)]
pub extern "C" fn system(_: *const c_char) -> c_int {
    -1
}
#[cfg(target_os = "wasi")]
#[unsafe(no_mangle)]
pub extern "C" fn mkstemp(_: *mut c_char) -> c_int {
    -1
}
