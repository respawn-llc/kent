use std::env;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use tauri::Manager;
use tauri_plugin_dialog::DialogExt;

mod native_glass;

const CONFIG_FILE_NAME: &str = "config.toml";
const DEFAULT_PERSISTENCE_ROOT: &str = "~/.kent";
const DEFAULT_SERVER_HOST: &str = "127.0.0.1";
const DEFAULT_SERVER_PORT: u16 = 53082;
const DEFAULT_THEME: &str = "auto";
const GUI_LOG_MAX_BYTES: u64 = 10 * 1024 * 1024;
const GUI_LOG_RETAIN_BYTES: u64 = 5 * 1024 * 1024;
const GUI_LOG_MAX_ENTRY_BYTES: usize = 64 * 1024;

#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
struct NativeContext {
    server_endpoint: String,
    persistence_root: String,
    platform: String,
    theme: String,
    home_path: String,
}

#[tauri::command]
fn resolve_native_context() -> Result<NativeContext, String> {
    native_context()
}

#[tauri::command]
fn resolve_native_platform() -> String {
    platform().to_string()
}

#[tauri::command]
async fn select_directory(app: tauri::AppHandle, title: String) -> Result<Option<String>, String> {
    let (sender, receiver) = tokio::sync::oneshot::channel();
    app.dialog()
        .file()
        .set_title(title)
        .pick_folder(move |selection| {
            let result = selection
                .map(|path| {
                    path.into_path()
                        .map(|path| path.to_string_lossy().to_string())
                        .map_err(|error| format!("Directory picker returned invalid path: {error}"))
                })
                .transpose();
            let _ = sender.send(result);
        });
    receiver
        .await
        .map_err(|_| "Directory picker closed before returning a result.".to_string())?
}

#[tauri::command]
fn open_external_url(url: String) -> Result<(), String> {
    validate_external_url(&url)?;
    tauri_plugin_opener::open_url(url, None::<&str>)
        .map_err(|error| format!("Open external link failed: {error}"))
}

#[tauri::command]
fn append_gui_log(entry: String) -> Result<(), String> {
    let entry_bytes = entry.as_bytes();
    if entry_bytes.len() > GUI_LOG_MAX_ENTRY_BYTES {
        return Err("GUI log entry exceeds maximum size.".to_string());
    }
    let path = gui_log_path()?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent)
            .map_err(|error| format!("Create GUI log directory failed: {error}"))?;
    }
    trim_log_if_needed(&path, entry_bytes.len() as u64 + 1)?;
    let mut file = OpenOptions::new()
        .create(true)
        .append(true)
        .open(&path)
        .map_err(|error| format!("Open GUI log failed: {error}"))?;
    file.write_all(entry_bytes)
        .and_then(|()| file.write_all(b"\n"))
        .map_err(|error| format!("Write GUI log failed: {error}"))
}

#[tauri::command]
async fn apply_native_window_glass(
    app: tauri::AppHandle,
    label: String,
) -> Result<native_glass::NativeGlassStatus, String> {
    native_glass::apply_to_label(app, label).await
}

#[tauri::command]
async fn set_native_window_glass_tint(
    app: tauri::AppHandle,
    label: String,
    tint: Option<native_glass::NativeGlassTint>,
) -> Result<native_glass::NativeGlassStatus, String> {
    native_glass::set_tint_for_label(app, label, tint).await
}

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .setup(|app| {
            #[cfg(target_os = "macos")]
            if let Some(window) = app.get_webview_window("main") {
                if let Err(error) = native_glass::apply_to_window_now(&window) {
                    eprintln!("Apply native window glass failed: {error}");
                }
            }
            // On Linux we paint our own window chrome (see `AppChrome`), so we
            // request client-side decorations. This stops the compositor from
            // drawing its own titlebar, and — critically for Wayland compositors
            // such as Niri whose `draw-border-with-background` default fills a
            // solid rectangle behind windows that omit CSD — keeps the focus
            // ring/border from showing through our semi-transparent window as a
            // full-window backdrop. The window stays transparent; only the
            // decoration mode changes.
            #[cfg(target_os = "linux")]
            if let Some(window) = app.get_webview_window("main") {
                if let Err(error) = window.set_decorations(false) {
                    eprintln!("Disable Linux window decorations failed: {error}");
                }
            }
            Ok(())
        })
        .invoke_handler(tauri::generate_handler![
            resolve_native_context,
            resolve_native_platform,
            select_directory,
            open_external_url,
            append_gui_log,
            apply_native_window_glass,
            set_native_window_glass_tint,
        ])
        .run(tauri::generate_context!())
        .expect("error while running the desktop application");
}

fn native_context() -> Result<NativeContext, String> {
    let settings = load_settings()?;
    let home_path = home_dir()?.to_string_lossy().to_string();
    Ok(NativeContext {
        server_endpoint: server_rpc_url(&settings.server_host, settings.server_port),
        persistence_root: settings.persistence_root.to_string_lossy().to_string(),
        platform: platform().to_string(),
        theme: settings.theme,
        home_path,
    })
}

fn platform() -> &'static str {
    match std::env::consts::OS {
        "linux" => "linux",
        "macos" => "macos",
        "windows" => "windows",
        _ => "unknown",
    }
}

struct Settings {
    server_host: String,
    server_port: u16,
    persistence_root: PathBuf,
    theme: String,
}

fn load_settings() -> Result<Settings, String> {
    let mut server_host = DEFAULT_SERVER_HOST.to_string();
    let mut server_port = DEFAULT_SERVER_PORT;
    let mut persistence_root = resolve_configured_path(DEFAULT_PERSISTENCE_ROOT)?;
    let mut theme = DEFAULT_THEME.to_string();

    if let Some(config) = read_home_config()? {
        if let Some(value) = config.get("server_host").and_then(toml::Value::as_str) {
            if !value.trim().is_empty() {
                server_host = value.trim().to_string();
            }
        }
        if let Some(value) = config.get("server_port").and_then(toml::Value::as_integer) {
            server_port = parse_server_port(value)?;
        }
        if let Some(value) = config.get("persistence_root").and_then(toml::Value::as_str) {
            if !value.trim().is_empty() {
                persistence_root = resolve_configured_path(value)?;
            }
        }
        if let Some(value) = config.get("theme").and_then(toml::Value::as_str) {
            if !value.trim().is_empty() {
                theme = parse_theme(value, "theme")?;
            }
        }
    }

    if let Ok(value) = env::var("KENT_SERVER_HOST") {
        if !value.trim().is_empty() {
            server_host = value.trim().to_string();
        }
    }
    if let Ok(value) = env::var("KENT_SERVER_PORT") {
        server_port = parse_server_port_string(&value)?;
    }
    if let Ok(value) = env::var("KENT_PERSISTENCE_ROOT") {
        if !value.trim().is_empty() {
            persistence_root = resolve_configured_path(value.trim())?;
        }
    }
    if let Ok(value) = env::var("KENT_THEME") {
        if !value.trim().is_empty() {
            theme = parse_theme(&value, "KENT_THEME")?;
        }
    }

    if server_host.trim().is_empty() {
        return Err("server_host must not be empty.".to_string());
    }
    Ok(Settings {
        server_host,
        server_port,
        persistence_root,
        theme,
    })
}

fn read_home_config() -> Result<Option<toml::Value>, String> {
    let path = home_dir()?.join(".kent").join(CONFIG_FILE_NAME);
    let content = match fs::read_to_string(&path) {
        Ok(content) => content,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(format!("Read config failed: {error}")),
    };
    toml::from_str::<toml::Value>(&content)
        .map(Some)
        .map_err(|error| format!("Parse config failed: {error}"))
}

fn parse_server_port(value: i64) -> Result<u16, String> {
    if value <= 0 || value > u16::MAX as i64 {
        return Err("server_port must be between 1 and 65535.".to_string());
    }
    Ok(value as u16)
}

fn parse_server_port_string(value: &str) -> Result<u16, String> {
    let parsed = value
        .trim()
        .parse::<i64>()
        .map_err(|_| "KENT_SERVER_PORT must be between 1 and 65535.".to_string())?;
    parse_server_port(parsed)
}

fn parse_theme(value: &str, setting_name: &str) -> Result<String, String> {
    let normalized = value.trim().to_ascii_lowercase();
    match normalized.as_str() {
        "auto" | "light" | "dark" => Ok(normalized),
        _ => Err(format!(
            "{setting_name} must be one of auto, light, or dark."
        )),
    }
}

fn resolve_configured_path(value: &str) -> Result<PathBuf, String> {
    let trimmed = value.trim();
    if trimmed == "~" {
        return home_dir();
    }
    let expanded = if let Some(rest) = trimmed.strip_prefix("~/") {
        home_dir()?.join(rest)
    } else {
        PathBuf::from(trimmed)
    };
    if expanded.is_absolute() {
        return Ok(expanded);
    }
    env::current_dir()
        .map(|cwd| cwd.join(expanded))
        .map_err(|error| format!("Resolve path failed: {error}"))
}

fn home_dir() -> Result<PathBuf, String> {
    if let Some(home) = env::var_os("HOME").map(PathBuf::from) {
        return Ok(home);
    }
    if let Some(home) = env::var_os("USERPROFILE").map(PathBuf::from) {
        return Ok(home);
    }
    match (env::var_os("HOMEDRIVE"), env::var_os("HOMEPATH")) {
        (Some(drive), Some(path)) => Ok(PathBuf::from(drive).join(path)),
        _ => Err("HOME is not set; cannot resolve paths.".to_string()),
    }
}

fn server_rpc_url(host: &str, port: u16) -> String {
    let authority_host = if host.contains(':') && !host.starts_with('[') && !host.ends_with(']') {
        format!("[{host}]")
    } else {
        host.to_string()
    };
    format!("ws://{authority_host}:{port}/rpc")
}

fn validate_external_url(url: &str) -> Result<(), String> {
    let (scheme, _) = url
        .split_once(':')
        .ok_or_else(|| "External link URL is missing a scheme.".to_string())?;
    match scheme.to_ascii_lowercase().as_str() {
        "http" | "https" | "mailto" => Ok(()),
        _ => Err("External link protocol is not allowed.".to_string()),
    }
}

fn gui_log_path() -> Result<PathBuf, String> {
    Ok(PathBuf::from(native_context()?.persistence_root)
        .join("gui")
        .join("desktop.log"))
}

fn trim_log_if_needed(path: &Path, append_bytes: u64) -> Result<(), String> {
    let metadata = match fs::metadata(path) {
        Ok(metadata) => metadata,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(()),
        Err(error) => return Err(format!("Read GUI log metadata failed: {error}")),
    };
    if metadata.len().saturating_add(append_bytes) <= GUI_LOG_MAX_BYTES {
        return Ok(());
    }
    let retain_bytes = GUI_LOG_RETAIN_BYTES.min(metadata.len());
    let mut file =
        File::open(path).map_err(|error| format!("Open GUI log for trimming failed: {error}"))?;
    file.seek(SeekFrom::End(-(retain_bytes as i64)))
        .map_err(|error| format!("Seek GUI log failed: {error}"))?;
    let mut retained = Vec::new();
    file.read_to_end(&mut retained)
        .map_err(|error| format!("Read GUI log tail failed: {error}"))?;
    fs::write(path, retained).map_err(|error| format!("Trim GUI log failed: {error}"))
}

#[cfg(test)]
mod tests {
    use super::{parse_theme, server_rpc_url};

    #[test]
    fn server_rpc_url_brackets_ipv6_hosts() {
        assert_eq!(server_rpc_url("::1", 65432), "ws://[::1]:65432/rpc");
    }

    #[test]
    fn server_rpc_url_uses_configured_remote_hosts() {
        assert_eq!(
            server_rpc_url("192.0.2.10", 53082),
            "ws://192.0.2.10:53082/rpc"
        );
    }

    #[test]
    fn parse_theme_accepts_supported_values_case_insensitively() {
        assert_eq!(parse_theme("auto", "theme").expect("auto theme"), "auto");
        assert_eq!(
            parse_theme(" Light ", "theme").expect("light theme"),
            "light"
        );
        assert_eq!(parse_theme("DARK", "theme").expect("dark theme"), "dark");
    }

    #[test]
    fn parse_theme_rejects_unknown_values() {
        assert_eq!(
            parse_theme("solarized", "KENT_THEME").expect_err("invalid theme"),
            "KENT_THEME must be one of auto, light, or dark.",
        );
    }
}
