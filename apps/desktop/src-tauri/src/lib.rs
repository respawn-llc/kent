use std::env;
use std::fs::{self, File, OpenOptions};
use std::io::{Read, Seek, SeekFrom, Write};
use std::path::{Path, PathBuf};
use std::process::Command;
use tauri_plugin_dialog::DialogExt;

const BUILDER_CONFIG_NAME: &str = "config.toml";
const DEFAULT_PERSISTENCE_ROOT: &str = "~/.builder";
const DEFAULT_SERVER_HOST: &str = "127.0.0.1";
const DEFAULT_SERVER_PORT: u16 = 53082;
const DEFAULT_THEME: &str = "auto";
const GUI_LOG_MAX_BYTES: u64 = 10 * 1024 * 1024;
const GUI_LOG_RETAIN_BYTES: u64 = 5 * 1024 * 1024;
const GUI_LOG_MAX_ENTRY_BYTES: usize = 64 * 1024;

#[derive(serde::Serialize)]
#[serde(rename_all = "camelCase")]
struct BuilderNativeContext {
    server_endpoint: String,
    persistence_root: String,
    theme: String,
}

#[tauri::command]
fn resolve_builder_context() -> Result<BuilderNativeContext, String> {
    builder_native_context()
}

#[tauri::command]
async fn select_directory(app: tauri::AppHandle, title: String) -> Result<Option<String>, String> {
    let (sender, receiver) = tokio::sync::oneshot::channel();
    app.dialog().file().set_title(title).pick_folder(move |selection| {
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
fn launch_builder_session(session_id: String, cwd: String) -> Result<(), String> {
    if session_id.trim().is_empty() {
        return Err("Session ID is required.".to_string());
    }
    let cwd_path = Path::new(&cwd);
    if !cwd_path.is_dir() {
        return Err("Teleport working directory does not exist.".to_string());
    }
    ensure_builder_executable_available()?;

    launch_builder_session_impl(&session_id, &cwd)
}

#[tauri::command]
fn append_gui_log(entry: String) -> Result<(), String> {
    let entry_bytes = entry.as_bytes();
    if entry_bytes.len() > GUI_LOG_MAX_ENTRY_BYTES {
        return Err("GUI log entry exceeds maximum size.".to_string());
    }
    let path = gui_log_path()?;
    if let Some(parent) = path.parent() {
        fs::create_dir_all(parent).map_err(|error| format!("Create GUI log directory failed: {error}"))?;
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

#[cfg_attr(mobile, tauri::mobile_entry_point)]
pub fn run() {
    tauri::Builder::default()
        .plugin(tauri_plugin_clipboard_manager::init())
        .plugin(tauri_plugin_dialog::init())
        .plugin(tauri_plugin_opener::init())
        .invoke_handler(tauri::generate_handler![
            resolve_builder_context,
            select_directory,
            open_external_url,
            launch_builder_session,
            append_gui_log,
        ])
        .run(tauri::generate_context!())
        .expect("error while running Builder desktop application");
}

#[cfg(target_os = "macos")]
fn launch_builder_session_impl(session_id: &str, cwd: &str) -> Result<(), String> {
    let command = builder_continue_command(session_id, cwd);
    let script = format!(
        "tell application \"Terminal\"\ndo script \"{}\"\nactivate\nend tell",
        escape_applescript_string(&command),
    );
    run_command(Command::new("osascript").arg("-e").arg(script), "launch Builder terminal session")
}

fn builder_continue_command(session_id: &str, cwd: &str) -> String {
    format!("cd {}; builder --continue {}", shell_quote(cwd), shell_quote(session_id))
}

fn ensure_builder_executable_available() -> Result<(), String> {
    match Command::new("builder").arg("--help").output() {
        Ok(_) => Ok(()),
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => {
            Err("Local Builder executable is unavailable.".to_string())
        }
        Err(error) => Err(format!("Check local Builder executable failed: {error}")),
    }
}

fn builder_native_context() -> Result<BuilderNativeContext, String> {
    let settings = load_builder_settings()?;
    Ok(BuilderNativeContext {
        server_endpoint: server_rpc_url(&settings.server_host, settings.server_port),
        persistence_root: settings.persistence_root.to_string_lossy().to_string(),
        theme: settings.theme,
    })
}

struct BuilderSettings {
    server_host: String,
    server_port: u16,
    persistence_root: PathBuf,
    theme: String,
}

fn load_builder_settings() -> Result<BuilderSettings, String> {
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

    if let Ok(value) = env::var("BUILDER_SERVER_HOST") {
        if !value.trim().is_empty() {
            server_host = value.trim().to_string();
        }
    }
    if let Ok(value) = env::var("BUILDER_SERVER_PORT") {
        server_port = parse_server_port_string(&value)?;
    }
    if let Ok(value) = env::var("BUILDER_PERSISTENCE_ROOT") {
        if !value.trim().is_empty() {
            persistence_root = resolve_configured_path(value.trim())?;
        }
    }
    if let Ok(value) = env::var("BUILDER_THEME") {
        if !value.trim().is_empty() {
            theme = parse_theme(&value, "BUILDER_THEME")?;
        }
    }

    if server_host.trim().is_empty() {
        return Err("server_host must not be empty.".to_string());
    }
    Ok(BuilderSettings {
        server_host,
        server_port,
        persistence_root,
        theme,
    })
}

fn read_home_config() -> Result<Option<toml::Value>, String> {
    let path = home_dir()?.join(".builder").join(BUILDER_CONFIG_NAME);
    let content = match fs::read_to_string(&path) {
        Ok(content) => content,
        Err(error) if error.kind() == std::io::ErrorKind::NotFound => return Ok(None),
        Err(error) => return Err(format!("Read Builder config failed: {error}")),
    };
    toml::from_str::<toml::Value>(&content)
        .map(Some)
        .map_err(|error| format!("Parse Builder config failed: {error}"))
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
        .map_err(|_| "BUILDER_SERVER_PORT must be between 1 and 65535.".to_string())?;
    parse_server_port(parsed)
}

fn parse_theme(value: &str, setting_name: &str) -> Result<String, String> {
    let normalized = value.trim().to_ascii_lowercase();
    match normalized.as_str() {
        "auto" | "light" | "dark" => Ok(normalized),
        _ => Err(format!("{setting_name} must be one of auto, light, or dark.")),
    }
}

fn resolve_configured_path(value: &str) -> Result<PathBuf, String> {
    let trimmed = value.trim();
    if trimmed == "~" {
        return Ok(home_dir()?);
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
        .map_err(|error| format!("Resolve Builder path failed: {error}"))
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
        _ => Err("HOME is not set; cannot resolve Builder paths.".to_string()),
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

#[cfg(not(target_os = "macos"))]
fn launch_builder_session_impl(_session_id: &str, _cwd: &str) -> Result<(), String> {
    Err("Terminal teleport is not implemented for this platform.".to_string())
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

fn run_command(command: &mut Command, action: &str) -> Result<(), String> {
    let output = command
        .output()
        .map_err(|error| format!("{action} failed: {error}"))?;
    if output.status.success() {
        return Ok(());
    }
    Err(command_error(action, &output.stderr))
}

fn command_error(action: &str, stderr: &[u8]) -> String {
    let message = String::from_utf8_lossy(stderr).trim().to_string();
    if message.is_empty() {
        return format!("{action} failed.");
    }
    format!("{action} failed: {message}")
}

fn escape_applescript_string(value: &str) -> String {
    let mut escaped = String::with_capacity(value.len());
    for character in value.chars() {
        match character {
            '\\' => escaped.push_str("\\\\"),
            '"' => escaped.push_str("\\\""),
            '\n' => escaped.push_str("\\n"),
            '\r' => escaped.push_str("\\r"),
            _ => escaped.push(character),
        }
    }
    escaped
}

fn shell_quote(value: &str) -> String {
    let mut quoted = String::with_capacity(value.len() + 2);
    quoted.push('\'');
    for character in value.chars() {
        if character == '\'' {
            quoted.push_str("'\\''");
        } else {
            quoted.push(character);
        }
    }
    quoted.push('\'');
    quoted
}

fn gui_log_path() -> Result<PathBuf, String> {
    Ok(PathBuf::from(builder_native_context()?.persistence_root).join("gui").join("desktop.log"))
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
    let mut file = File::open(path).map_err(|error| format!("Open GUI log for trimming failed: {error}"))?;
    file.seek(SeekFrom::End(-(retain_bytes as i64)))
        .map_err(|error| format!("Seek GUI log failed: {error}"))?;
    let mut retained = Vec::new();
    file.read_to_end(&mut retained)
        .map_err(|error| format!("Read GUI log tail failed: {error}"))?;
    fs::write(path, retained).map_err(|error| format!("Trim GUI log failed: {error}"))
}

#[cfg(test)]
mod tests {
    use super::{builder_continue_command, parse_theme, server_rpc_url, shell_quote};

    #[test]
    fn builder_terminal_command_uses_interactive_continue() {
        let command = builder_continue_command("session-1", "/tmp/worktree");

        assert!(command.contains("builder --continue 'session-1'"));
        assert!(!command.contains("builder run --continue"));
    }

    #[test]
    fn shell_quote_handles_single_quotes() {
        assert_eq!(shell_quote("/tmp/nek's worktree"), "'/tmp/nek'\\''s worktree'");
    }

    #[test]
    fn server_rpc_url_brackets_ipv6_hosts() {
        assert_eq!(server_rpc_url("::1", 65432), "ws://[::1]:65432/rpc");
    }

    #[test]
    fn server_rpc_url_uses_configured_remote_hosts() {
        assert_eq!(server_rpc_url("192.0.2.10", 53082), "ws://192.0.2.10:53082/rpc");
    }

    #[test]
    fn parse_theme_accepts_supported_values_case_insensitively() {
        assert_eq!(parse_theme("auto", "theme").expect("auto theme"), "auto");
        assert_eq!(parse_theme(" Light ", "theme").expect("light theme"), "light");
        assert_eq!(parse_theme("DARK", "theme").expect("dark theme"), "dark");
    }

    #[test]
    fn parse_theme_rejects_unknown_values() {
        assert_eq!(
            parse_theme("solarized", "BUILDER_THEME").expect_err("invalid theme"),
            "BUILDER_THEME must be one of auto, light, or dark.",
        );
    }
}
