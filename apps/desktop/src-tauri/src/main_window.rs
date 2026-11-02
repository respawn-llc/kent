use std::sync::Mutex;
use tauri::{Emitter, Manager};

use crate::AttentionNotificationActivation;

#[derive(Clone, serde::Serialize, serde::Deserialize)]
#[serde(tag = "kind")]
pub enum Destination {
    #[serde(rename = "home")]
    Home,
    #[serde(rename = "sessionChat", rename_all = "camelCase")]
    SessionChat {
        project_id: String,
        session_id: String,
    },
    #[serde(rename = "taskDetail", rename_all = "camelCase")]
    TaskDetail { task_id: String },
}

#[derive(Default)]
pub struct State {
    opening: Mutex<()>,
    delivery: Mutex<MainWindow>,
    startup_finished: Mutex<bool>,
}

impl State {
    pub fn closed(&self) {
        self.finish_startup();
        *self.delivery.lock().expect("Main window state poisoned") = MainWindow::default();
    }

    pub fn initial_startup(&self) -> bool {
        !*self
            .startup_finished
            .lock()
            .expect("Startup state poisoned")
    }

    pub fn finish_startup(&self) {
        *self
            .startup_finished
            .lock()
            .expect("Startup state poisoned") = true;
    }

    pub fn connect<R: tauri::Runtime>(
        &self,
        app: &tauri::AppHandle<R>,
        owner: &crate::settings::SettingsOwner,
        launch: &crate::settings::DesktopLaunch,
        address: &str,
    ) -> Result<crate::settings::DesktopSettings, crate::settings::SettingsError> {
        let finished = self
            .startup_finished
            .lock()
            .expect("Startup state poisoned");
        if *finished {
            return Err(crate::settings::SettingsError::Storage(
                "Initial main-window startup has ended; restart to change servers.".into(),
            ));
        }
        owner.connect(
            app,
            launch,
            address,
            crate::settings::ConnectionMode::MainStartup,
        )
    }
}

#[tauri::command]
pub fn complete_main_startup(window: tauri::WebviewWindow, state: tauri::State<'_, State>) {
    if window.label() == "main" {
        state.finish_startup();
    }
}

#[derive(Default)]
struct MainWindow {
    navigation: Delivery<Destination>,
    notification: Delivery<AttentionNotificationActivation>,
}

struct Delivery<T> {
    ready: bool,
    pending: Option<T>,
}

impl<T> Default for Delivery<T> {
    fn default() -> Self {
        Self {
            ready: false,
            pending: None,
        }
    }
}

impl<T: Clone + serde::Serialize> Delivery<T> {
    fn send(&mut self, app: &tauri::AppHandle, event: &str, value: T) -> Result<(), String> {
        if self.ready {
            app.emit_to("main", event, value)
                .map_err(|error| error.to_string())
        } else {
            // Navigation is a destination, not a queue of work: the latest click wins.
            self.pending = Some(value);
            Ok(())
        }
    }

    fn ready(&mut self, app: &tauri::AppHandle, event: &str) -> Result<(), String> {
        if let Some(value) = &self.pending {
            app.emit_to("main", event, value)
                .map_err(|error| error.to_string())?;
        }
        self.pending = None;
        self.ready = true;
        Ok(())
    }
}

const NAVIGATION: &str = "app://main-navigation";
const NOTIFICATION: &str = "app://attention-notification-activated";

pub fn create(app: &tauri::AppHandle) -> Result<tauri::WebviewWindow, String> {
    let config = app
        .config()
        .app
        .windows
        .iter()
        .find(|config| config.label == "main")
        .ok_or("Main window configuration is missing")?;
    let mut config = config.clone();
    config.transparent = app.state::<crate::settings::DesktopLaunch>().transparent();
    let window = tauri::WebviewWindowBuilder::from_config(app, &config)
        .map_err(|error| error.to_string())?
        .build()
        .map_err(|error| error.to_string())?;
    #[cfg(any(target_os = "macos", windows))]
    if config.transparent {
        let glass_window = window.clone();
        window
            .run_on_main_thread(move || {
                if let Err(error) = crate::native_glass::apply_to_window_now(&glass_window) {
                    eprintln!("Apply native window glass failed: {error}");
                }
            })
            .map_err(|error| error.to_string())?;
    }
    Ok(window)
}

fn show(app: &tauri::AppHandle) -> Result<(), String> {
    let window = match app.get_webview_window("main") {
        Some(window) => window,
        None => create(app)?,
    };
    window.unminimize().map_err(|error| error.to_string())?;
    window.show().map_err(|error| error.to_string())?;
    window.set_focus().map_err(|error| error.to_string())
}

#[tauri::command]
pub async fn open_main_window(
    app: tauri::AppHandle,
    destination: Option<Destination>,
) -> Result<(), String> {
    open(&app, destination.map(Request::Navigation))
}

enum Request {
    Navigation(Destination),
    Notification(AttentionNotificationActivation),
}

fn open(app: &tauri::AppHandle, request: Option<Request>) -> Result<(), String> {
    let state = app.state::<State>();
    // Window creation dispatches to the UI thread. Its lifecycle callbacks must
    // remain free to reset delivery without waiting for creation to finish.
    let _opening = state.opening.lock().map_err(|error| error.to_string())?;
    show(app)?;
    let mut delivery = state.delivery.lock().map_err(|error| error.to_string())?;
    match request {
        Some(Request::Navigation(destination)) => {
            delivery.navigation.send(app, NAVIGATION, destination)
        }
        Some(Request::Notification(activation)) => {
            delivery.notification.send(app, NOTIFICATION, activation)
        }
        None => Ok(()),
    }
}

#[derive(serde::Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum Channel {
    Navigation,
    Notification,
}

#[tauri::command]
pub async fn main_window_ready(
    app: tauri::AppHandle,
    window: tauri::WebviewWindow,
    channel: Channel,
) -> Result<(), String> {
    if window.label() != "main" {
        return Err("Only the main window can register navigation delivery".into());
    }
    let state = app.state::<State>();
    let mut state = state.delivery.lock().map_err(|error| error.to_string())?;
    match channel {
        Channel::Navigation => state.navigation.ready(&app, NAVIGATION),
        Channel::Notification => state.notification.ready(&app, NOTIFICATION),
    }
}

pub fn activate(app: &tauri::AppHandle, activation: AttentionNotificationActivation) {
    let app = app.clone();
    tauri::async_runtime::spawn(async move {
        if let Err(error) = open(&app, Some(Request::Notification(activation))) {
            eprintln!("Open attention notification failed: {error}");
        }
    });
}
