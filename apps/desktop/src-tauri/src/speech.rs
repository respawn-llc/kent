use futures_util::{
    future::{BoxFuture, Shared},
    FutureExt,
};
use std::collections::HashMap;
use std::fs::File;
use std::io::Write;
use std::path::PathBuf;
use std::sync::{
    atomic::{AtomicU64, Ordering},
    Mutex,
};
use tauri::{Manager, State, WebviewWindow};
use tauri_plugin_dialog::DialogExt;
use tokio::sync::mpsc;
use tokio_util::{io::ReaderStream, sync::CancellationToken};

#[derive(Default)]
pub struct SpeechState {
    next: AtomicU64,
    recordings: Mutex<HashMap<u64, Recording>>,
}

struct Recording {
    owner: String,
    file: Option<File>,
    source: Original,
    max_bytes: u64,
    cancel: CancellationToken,
    upload: Option<Upload>,
}

struct Upload {
    sender: Option<mpsc::Sender<Vec<u8>>>,
    result: Shared<BoxFuture<'static, Result<HttpReply, String>>>,
}

impl Drop for Recording {
    fn drop(&mut self) {
        self.cancel.cancel();
    }
}

#[derive(Clone, serde::Serialize)]
#[serde(tag = "kind", rename_all = "camelCase")]
pub enum HttpReply {
    Response { status: u16, body: Vec<u8> },
    Interrupted { diagnostic: String },
}

#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct Original {
    path: PathBuf,
    mime_type: String,
    bytes: u64,
}

#[derive(serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub struct CaptureOptions {
    mime_type: String,
    extension: String,
    max_bytes: u64,
    upload_url: String,
}

#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Capture {
    handle: u64,
    path: PathBuf,
}

#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
pub struct Written {
    bytes: u64,
    limit_reached: bool,
}

fn owned<'a>(
    recordings: &'a mut HashMap<u64, Recording>,
    handle: u64,
    window: &WebviewWindow,
) -> Result<&'a mut Recording, String> {
    let recording = recordings
        .get_mut(&handle)
        .ok_or("Recording is no longer open")?;
    if recording.owner != window.label() {
        return Err("Recording belongs to another window".into());
    }
    Ok(recording)
}

#[tauri::command]
pub async fn speech_create(
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    options: CaptureOptions,
) -> Result<Capture, String> {
    if options.max_bytes == 0
        || options.mime_type.trim().is_empty()
        || options.extension.is_empty()
        || !options.extension.bytes().all(|b| b.is_ascii_alphanumeric())
    {
        return Err("Invalid recording encoding or limit".into());
    }
    let temporary = tempfile::Builder::new()
        .prefix("kent-speech-")
        .suffix(&format!(".{}", options.extension))
        .tempfile()
        .map_err(|e| e.to_string())?;
    // Keep one original in the OS temporary directory, including on window
    // termination. Explicit successful delivery/Discard removes it.
    let path = temporary.path().to_path_buf();
    let handle = state.next.fetch_add(1, Ordering::Relaxed);
    let source = Original {
        path: path.clone(),
        mime_type: options.mime_type,
        bytes: 0,
    };
    let cancel = CancellationToken::new();
    let (sender, receiver) = mpsc::channel::<Vec<u8>>(4);
    let stream = futures_util::stream::unfold(receiver, |mut receiver| async {
        receiver
            .recv()
            .await
            .map(|bytes| (Ok::<_, std::io::Error>(bytes), receiver))
    });
    let upload = start_upload(
        &options.upload_url,
        &source.mime_type,
        reqwest::Body::wrap_stream(stream),
        cancel.clone(),
    )?;
    let (file, _) = temporary.keep().map_err(|e| {
        cancel.cancel();
        e.to_string()
    })?;
    state.recordings.lock().map_err(|e| e.to_string())?.insert(
        handle,
        Recording {
            owner: window.label().to_owned(),
            file: Some(file),
            source,
            max_bytes: options.max_bytes,
            cancel,
            upload: Some(Upload {
                sender: Some(sender),
                result: upload,
            }),
        },
    );
    Ok(Capture { handle, path })
}

#[tauri::command]
pub async fn speech_append(
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    handle: u64,
    bytes: Vec<u8>,
) -> Result<Written, String> {
    let mut recordings = state.recordings.lock().map_err(|e| e.to_string())?;
    let recording = owned(&mut recordings, handle, &window)?;
    let file = recording
        .file
        .as_mut()
        .ok_or("Recording is already finalized")?;
    file.write_all(&bytes).map_err(|e| {
        format!(
            "Recording write failed at {}: {e}",
            recording.source.path.display()
        )
    })?;
    recording.source.bytes += bytes.len() as u64;
    if let Some(upload) = recording.upload.as_mut() {
        if let Some(sender) = upload.sender.as_ref() {
            for chunk in bytes.chunks(64 * 1024) {
                match sender.try_send(chunk.to_vec()) {
                    Ok(()) => {}
                    Err(mpsc::error::TrySendError::Full(_)) => {
                        // Backpressure ends transfer, never capture or the original.
                        recording.cancel.cancel();
                        upload.sender = None;
                        break;
                    }
                    Err(mpsc::error::TrySendError::Closed(_)) => {
                        // Keep the completed reply (including provider rejection)
                        // for Stop; a closed sender alone does not authorize replay.
                        upload.sender = None;
                        break;
                    }
                }
            }
        }
    }
    Ok(Written {
        bytes: recording.source.bytes,
        limit_reached: recording.source.bytes >= recording.max_bytes,
    })
}

#[tauri::command]
pub async fn speech_finalize(
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    handle: u64,
) -> Result<Original, String> {
    let mut recordings = state.recordings.lock().map_err(|e| e.to_string())?;
    let recording = owned(&mut recordings, handle, &window)?;
    if let Some(mut file) = recording.file.take() {
        file.flush().map_err(|e| {
            format!(
                "Recording flush failed at {}: {e}",
                recording.source.path.display()
            )
        })?;
        recording.source.bytes = file.metadata().map_err(|e| e.to_string())?.len();
    }
    if let Some(upload) = recording.upload.as_mut() {
        upload.sender = None;
    }
    if recording.source.bytes > recording.max_bytes {
        recording.cancel.cancel();
    }
    Ok(recording.source.clone())
}

fn start_upload(
    url: &str,
    mime_type: &str,
    body: reqwest::Body,
    cancel: CancellationToken,
) -> Result<Shared<BoxFuture<'static, Result<HttpReply, String>>>, String> {
    let client = reqwest::Client::builder()
        .redirect(reqwest::redirect::Policy::none())
        .build()
        .map_err(|e| e.to_string())?;
    let request = client
        .post(url)
        .header(reqwest::header::CONTENT_TYPE, mime_type)
        .body(body)
        .build()
        .map_err(|e| e.to_string())?;
    Ok(tokio::spawn(async move {
        tokio::select! {
            _ = cancel.cancelled() => Ok(HttpReply::Interrupted { diagnostic: "Speech transfer canceled or unable to keep up with capture".into() }),
            result = receive_reply(client, request) => result,
        }
    }).map(|result| result.map_err(|e| e.to_string()).and_then(|reply| reply)).boxed().shared())
}

async fn receive_reply(
    client: reqwest::Client,
    request: reqwest::Request,
) -> Result<HttpReply, String> {
    let response = client.execute(request).await;
    let mut response = match response {
        Ok(response) => response,
        Err(error) => {
            return Ok(HttpReply::Interrupted {
                diagnostic: error.without_url().to_string(),
            })
        }
    };
    let status = response.status().as_u16();
    let mut body = Vec::new();
    loop {
        let chunk = match response.chunk().await {
            Ok(chunk) => chunk,
            Err(error) => {
                return Ok(HttpReply::Interrupted {
                    diagnostic: error.without_url().to_string(),
                })
            }
        };
        let Some(chunk) = chunk else { break };
        if body.len() + chunk.len() > 1024 * 1024 {
            return Err("Speech response exceeds the maximum response size".into());
        }
        body.extend_from_slice(&chunk);
    }
    Ok(HttpReply::Response { status, body })
}

#[tauri::command]
pub async fn speech_finish(
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    handle: u64,
) -> Result<HttpReply, String> {
    let result = {
        let mut recordings = state.recordings.lock().map_err(|e| e.to_string())?;
        let recording = owned(&mut recordings, handle, &window)?;
        if recording.file.is_some() {
            return Err("Recording must be finalized before awaiting transcription".into());
        }
        recording
            .upload
            .as_ref()
            .ok_or("Speech transfer is unavailable")?
            .result
            .clone()
    };
    let reply = result.await;
    state
        .recordings
        .lock()
        .map_err(|e| e.to_string())?
        .remove(&handle);
    reply
}

#[tauri::command]
pub async fn speech_cancel(
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    handle: u64,
) -> Result<(), String> {
    let result = {
        let mut recordings = state.recordings.lock().map_err(|e| e.to_string())?;
        if !recordings.contains_key(&handle) {
            return Ok(());
        }
        let recording = owned(&mut recordings, handle, &window)?;
        let result = recording
            .upload
            .as_ref()
            .map(|upload| upload.result.clone());
        recordings.remove(&handle);
        result
    };
    if let Some(result) = result {
        result.await?;
    }
    Ok(())
}

#[tauri::command]
pub async fn speech_upload_original(
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    original: Original,
    upload_url: String,
    max_bytes: u64,
) -> Result<u64, String> {
    let file = tokio::fs::File::open(&original.path)
        .await
        .map_err(|e| e.to_string())?;
    let bytes = file.metadata().await.map_err(|e| e.to_string())?.len();
    if bytes > max_bytes {
        return Err("Finalized recording exceeds the current upload limit".into());
    }
    let cancel = CancellationToken::new();
    let result = start_upload(
        &upload_url,
        &original.mime_type,
        reqwest::Body::wrap_stream(ReaderStream::new(file)),
        cancel.clone(),
    )?;
    let handle = state.next.fetch_add(1, Ordering::Relaxed);
    state.recordings.lock().map_err(|e| e.to_string())?.insert(
        handle,
        Recording {
            owner: window.label().to_owned(),
            file: None,
            source: Original { bytes, ..original },
            max_bytes,
            cancel,
            upload: Some(Upload {
                sender: None,
                result,
            }),
        },
    );
    Ok(handle)
}

#[tauri::command]
pub async fn speech_discard(path: PathBuf) -> Result<(), String> {
    std::fs::remove_file(path).map_err(|e| e.to_string())
}

#[derive(serde::Serialize)]
#[serde(tag = "kind", rename_all = "kebab-case")]
pub enum SaveCopyOutcome {
    Saved,
    Canceled,
    OriginalPath,
}

#[tauri::command]
pub async fn speech_save_copy(
    app: tauri::AppHandle,
    path: PathBuf,
    title: String,
) -> Result<SaveCopyOutcome, String> {
    let filename = path
        .file_name()
        .ok_or("Recording filename is missing")?
        .to_string_lossy()
        .to_string();
    let (sender, receiver) = tokio::sync::oneshot::channel();
    app.dialog()
        .file()
        .set_title(title)
        .set_file_name(filename)
        .save_file(move |chosen| {
            let _ = sender.send(chosen);
        });
    let chosen = receiver.await.map_err(|e| e.to_string())?;
    let Some(chosen) = chosen else {
        return Ok(SaveCopyOutcome::Canceled);
    };
    let target = chosen.into_path().map_err(|e| e.to_string())?;
    if target == path {
        return Ok(SaveCopyOutcome::OriginalPath);
    }
    let parent = target
        .parent()
        .ok_or("Recording copy needs a parent directory")?;
    let temporary = tempfile::NamedTempFile::new_in(parent).map_err(|e| e.to_string())?;
    std::fs::copy(path, temporary.path()).map_err(|e| e.to_string())?;
    temporary.persist(&target).map_err(|e| e.to_string())?;
    Ok(SaveCopyOutcome::Saved)
}

#[tauri::command]
pub fn speech_limit_sound() -> Result<bool, String> {
    #[cfg(target_os = "macos")]
    {
        objc2_app_kit::NSBeep();
        Ok(true)
    }
    #[cfg(windows)]
    {
        use windows::Win32::System::Diagnostics::Debug::MessageBeep;
        use windows::Win32::UI::WindowsAndMessaging::MB_OK;
        unsafe { MessageBeep(MB_OK) }.map_err(|error| error.to_string())?;
        Ok(true)
    }
    #[cfg(target_os = "linux")]
    {
        if let Some(display) = gtk::gdk::Display::default() {
            // GDK accepts the bell request without reporting physical playback.
            display.beep();
            Ok(true)
        } else {
            Ok(false)
        }
    }
    #[cfg(not(any(target_os = "macos", target_os = "linux", windows)))]
    {
        Ok(false)
    }
}

pub fn close_window(window: &tauri::Window) {
    let state = window.state::<SpeechState>();
    let mut recordings = state
        .recordings
        .lock()
        .expect("speech resource lock poisoned");
    recordings.retain(|_, recording| {
        if recording.owner != window.label() {
            return true;
        }
        eprintln!(
            "Window closed; temporary recording retained at {}",
            recording.source.path.display()
        );
        false
    });
}
