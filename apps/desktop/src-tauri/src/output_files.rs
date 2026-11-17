use std::fs::{self, File};
use std::io::{BufReader, Read};
use std::path::Path;

const PREVIEW_MAX_BYTES: u64 = 20 * 1024 * 1024;

#[derive(serde::Serialize)]
#[serde(
    tag = "kind",
    rename_all = "snake_case",
    rename_all_fields = "camelCase"
)]
pub enum FileInspection {
    Missing,
    File,
    Image,
    PreviewIneligible,
}

#[tauri::command]
pub async fn inspect_output_file(
    path: String,
    base_path: Option<String>,
) -> Result<FileInspection, String> {
    tauri::async_runtime::spawn_blocking(move || {
        inspect_file(&path, base_path.as_deref().unwrap_or(""))
    })
    .await
    .map_err(|error| format!("File inspection failed: {error}"))?
}

fn inspect_file(path: &str, base_path: &str) -> Result<FileInspection, String> {
    let resolved = super::resolve_user_file_path(path, base_path)?;
    match fs::metadata(&resolved) {
        Ok(metadata) if !metadata.is_file() => return Ok(FileInspection::Missing),
        Ok(_) => {}
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            return Ok(FileInspection::Missing)
        }
        Err(error) => return Err(format!("Read file metadata failed: {error}")),
    }
    let reader = open_image_reader(&resolved)?;
    if reader.format().is_none() {
        return Ok(FileInspection::File);
    }
    if reader
        .into_inner()
        .get_ref()
        .metadata()
        .map_err(|error| format!("Read image metadata failed: {error}"))?
        .len()
        > PREVIEW_MAX_BYTES
    {
        return Ok(FileInspection::PreviewIneligible);
    }
    Ok(FileInspection::Image)
}

fn open_image_reader(path: &Path) -> Result<image::ImageReader<BufReader<File>>, String> {
    File::open(path)
        .map(|file| image::ImageReader::new(BufReader::new(file)))
        .and_then(|reader| reader.with_guessed_format())
        .map_err(|error| format!("Inspect image format failed: {error}"))
}

#[derive(serde::Serialize)]
#[serde(
    tag = "kind",
    rename_all = "snake_case",
    rename_all_fields = "camelCase"
)]
pub enum ImagePreview {
    Missing,
    PreviewIneligible,
    Image { bytes: Vec<u8> },
}

#[tauri::command]
pub async fn read_output_image(
    path: String,
    base_path: Option<String>,
) -> Result<tauri::ipc::Response, String> {
    let preview = tauri::async_runtime::spawn_blocking(move || {
        read_image(&path, base_path.as_deref().unwrap_or(""))
    })
    .await
    .map_err(|error| format!("Image preview read failed: {error}"))??;
    match preview {
        ImagePreview::Image { bytes } => Ok(tauri::ipc::Response::new(bytes)),
        outcome => serde_json::to_string(&outcome)
            .map(tauri::ipc::Response::new)
            .map_err(|error| format!("Encode image preview outcome failed: {error}")),
    }
}

fn read_image(path: &str, base_path: &str) -> Result<ImagePreview, String> {
    let resolved = super::resolve_user_file_path(path, base_path)?;
    if matches!(inspect_file(path, base_path)?, FileInspection::Missing) {
        return Ok(ImagePreview::Missing);
    }
    let reader = open_image_reader(&resolved)?;
    if reader.format().is_none() {
        return Ok(ImagePreview::PreviewIneligible);
    }
    let mut bytes = Vec::new();
    reader
        .into_inner()
        .take(PREVIEW_MAX_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|error| format!("Read image preview failed: {error}"))?;
    if bytes.len() as u64 > PREVIEW_MAX_BYTES {
        return Ok(ImagePreview::PreviewIneligible);
    }
    Ok(ImagePreview::Image { bytes })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn inspects_exact_literal_and_relative_paths() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join("a%20#?.txt");
        std::fs::write(&path, b"text").unwrap();
        assert!(matches!(
            inspect_file("a%20#?.txt", root.path().to_str().unwrap()).unwrap(),
            FileInspection::File
        ));
        assert!(matches!(
            inspect_file(path.to_str().unwrap(), "").unwrap(),
            FileInspection::File
        ));
        assert!(matches!(
            inspect_file("missing.png", root.path().to_str().unwrap()).unwrap(),
            FileInspection::Missing
        ));
    }

    #[test]
    fn reads_only_eligible_images_within_the_preview_bound() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join("image.png");
        std::fs::write(&path, b"\x89PNG\r\n\x1a\n").unwrap();
        let file = File::options().write(true).open(&path).unwrap();
        file.set_len(PREVIEW_MAX_BYTES).unwrap();
        assert!(matches!(
            inspect_file(path.to_str().unwrap(), "").unwrap(),
            FileInspection::Image
        ));
        match read_image(path.to_str().unwrap(), "").unwrap() {
            ImagePreview::Image { bytes } => {
                assert_eq!(bytes.len() as u64, PREVIEW_MAX_BYTES);
            }
            _ => panic!("eligible image was not returned"),
        }
        file.set_len(PREVIEW_MAX_BYTES + 1).unwrap();
        assert!(matches!(
            read_image(path.to_str().unwrap(), "").unwrap(),
            ImagePreview::PreviewIneligible
        ));
        std::fs::write(&path, b"not an image").unwrap();
        assert!(matches!(
            read_image(path.to_str().unwrap(), "").unwrap(),
            ImagePreview::PreviewIneligible
        ));
    }
}
