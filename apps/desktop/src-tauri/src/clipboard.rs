use arboard::{Clipboard, Error};
use image::{codecs::png::PngEncoder, ImageEncoder};
use std::path::Path;

#[tauri::command]
pub async fn paste_clipboard() -> Result<Option<String>, String> {
    tauri::async_runtime::spawn_blocking(read_clipboard)
        .await
        .map_err(|error| format!("Clipboard reader failed: {error}"))?
}

fn read_clipboard() -> Result<Option<String>, String> {
    let mut clipboard = Clipboard::new().map_err(|error| error.to_string())?;
    match clipboard.get_image() {
        Ok(image) => return save_image(image).map(Some),
        Err(Error::ContentNotAvailable) => {}
        Err(error) => return Err(format!("Read clipboard image failed: {error}")),
    }
    match clipboard.get_text() {
        Ok(text) => Ok(Some(text)),
        Err(Error::ContentNotAvailable) => Ok(None),
        Err(error) => Err(format!("Read clipboard text failed: {error}")),
    }
}

fn save_image(image: arboard::ImageData<'_>) -> Result<String, String> {
    let width = u32::try_from(image.width).map_err(|error| error.to_string())?;
    let height = u32::try_from(image.height).map_err(|error| error.to_string())?;
    let temp_dir = if cfg!(unix) && Path::new("/tmp").is_dir() {
        Path::new("/tmp").to_path_buf()
    } else {
        std::env::temp_dir()
    };
    let mut file = tempfile::Builder::new()
        .prefix("kent-clipboard-")
        .suffix(".png")
        .tempfile_in(temp_dir)
        .map_err(|error| format!("Create clipboard image failed: {error}"))?;
    PngEncoder::new(file.as_file_mut())
        .write_image(&image.bytes, width, height, image::ExtendedColorType::Rgba8)
        .map_err(|error| format!("Save clipboard image failed: {error}"))?;
    let path = file
        .path()
        .to_str()
        .ok_or("Clipboard image path is not valid Unicode")?
        .to_owned();
    file.keep()
        .map_err(|error| format!("Keep clipboard image failed: {error}"))?;
    Ok(path)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::borrow::Cow;

    #[test]
    fn saved_clipboard_image_is_an_absolute_png_with_original_pixels() {
        let pixels = vec![12, 34, 56, 255, 78, 90, 12, 128];
        let path = save_image(arboard::ImageData {
            width: 2,
            height: 1,
            bytes: Cow::Borrowed(&pixels),
        })
        .unwrap();
        assert!(Path::new(&path).is_absolute());
        let decoded = image::open(&path).unwrap().into_rgba8();
        assert_eq!(decoded.dimensions(), (2, 1));
        assert_eq!(decoded.as_raw(), &pixels);
        std::fs::remove_file(path).unwrap();
    }
}
