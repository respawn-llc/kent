use std::fs::{self, File};
use std::io::{BufReader, Read};
use std::path::Path;

const PREVIEW_MAX_BYTES: u64 = 20 * 1024 * 1024;

#[derive(serde::Serialize)]
#[serde(tag = "kind", rename_all = "snake_case")]
pub enum OutputFile {
    Unavailable,
    Missing,
    File,
    PreviewIneligible,
    Image { bytes: Vec<u8> },
}

#[tauri::command]
pub async fn load_output_file(
    path: String,
    base_path: Option<String>,
) -> Result<tauri::ipc::Response, String> {
    let file = tauri::async_runtime::spawn_blocking(move || load_file(&path, base_path.as_deref()))
        .await
        .map_err(|error| format!("Output file load failed: {error}"))??;
    match file {
        OutputFile::Image { bytes } => Ok(tauri::ipc::Response::new(bytes)),
        outcome => serde_json::to_string(&outcome)
            .map(tauri::ipc::Response::new)
            .map_err(|error| format!("Encode output file outcome failed: {error}")),
    }
}

fn load_file(path: &str, base_path: Option<&str>) -> Result<OutputFile, String> {
    if Path::new(path).is_relative() && base_path.is_none() {
        return Ok(OutputFile::Unavailable);
    }
    let resolved = super::resolve_user_file_path(path, base_path.unwrap_or(""))?;
    match fs::metadata(&resolved) {
        Ok(metadata) if !metadata.is_file() => return Ok(OutputFile::Missing),
        Ok(_) => {}
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            return Ok(OutputFile::Missing)
        }
        Err(error) => return Err(format!("Read output file metadata failed: {error}")),
    }
    let file =
        File::open(&resolved).map_err(|error| format!("Open output file failed: {error}"))?;
    let metadata = file
        .metadata()
        .map_err(|error| format!("Read output file metadata failed: {error}"))?;
    let reader = image::ImageReader::new(BufReader::new(file))
        .with_guessed_format()
        .map_err(|error| format!("Inspect output image format failed: {error}"))?;
    if reader.format().is_none() {
        return Ok(OutputFile::File);
    }
    if metadata.len() > PREVIEW_MAX_BYTES {
        return Ok(OutputFile::PreviewIneligible);
    }
    let mut bytes = Vec::new();
    reader
        .into_inner()
        .take(PREVIEW_MAX_BYTES + 1)
        .read_to_end(&mut bytes)
        .map_err(|error| format!("Read output image failed: {error}"))?;
    if bytes.len() as u64 > PREVIEW_MAX_BYTES {
        return Ok(OutputFile::PreviewIneligible);
    }
    Ok(OutputFile::Image { bytes })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn loads_exact_literal_and_relative_files_without_requiring_a_base_for_absolute_paths() {
        let root = tempfile::tempdir().unwrap();
        // These characters are valid POSIX filenames; Windows path syntax is tested by
        // frontend classification, while actual filesystem resolution uses this platform.
        let name = if cfg!(windows) {
            "a%20#.txt"
        } else {
            "a%20#?.txt"
        };
        let path = root.path().join(name);
        std::fs::write(&path, b"text").unwrap();
        assert!(matches!(
            load_file(name, Some(root.path().to_str().unwrap())).unwrap(),
            OutputFile::File
        ));
        assert!(matches!(
            load_file(path.to_str().unwrap(), None).unwrap(),
            OutputFile::File
        ));
        assert!(matches!(
            load_file("missing.png", Some(root.path().to_str().unwrap())).unwrap(),
            OutputFile::Missing
        ));
        assert!(matches!(
            load_file(name, None).unwrap(),
            OutputFile::Unavailable
        ));
        assert!(matches!(
            load_file(root.path().to_str().unwrap(), None).unwrap(),
            OutputFile::Missing
        ));
    }

    #[test]
    fn loads_only_eligible_images_within_the_preview_bound() {
        let root = tempfile::tempdir().unwrap();
        let path = root.path().join("image.png");
        std::fs::write(&path, b"\x89PNG\r\n\x1a\n").unwrap();
        let file = File::options().write(true).open(&path).unwrap();
        file.set_len(PREVIEW_MAX_BYTES).unwrap();
        match load_file(path.to_str().unwrap(), None).unwrap() {
            OutputFile::Image { bytes } => assert_eq!(bytes.len() as u64, PREVIEW_MAX_BYTES),
            _ => panic!("eligible image was not returned"),
        }
        file.set_len(PREVIEW_MAX_BYTES + 1).unwrap();
        assert!(matches!(
            load_file(path.to_str().unwrap(), None).unwrap(),
            OutputFile::PreviewIneligible
        ));
        std::fs::write(&path, b"not an image").unwrap();
        assert!(matches!(
            load_file(path.to_str().unwrap(), None).unwrap(),
            OutputFile::File
        ));
    }
}
