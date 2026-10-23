use serde::{Deserialize, Serialize};
use std::{path::PathBuf, sync::Mutex};
use tauri::Emitter;
use tauri_plugin_store::StoreExt;
const SETTINGS_VERSION: u32 = 1;

#[derive(Clone, Copy, Debug, Serialize)]
#[serde(rename_all = "camelCase")]
pub struct LaunchAppearance {
    pub transparency: bool,
    pub low_power: bool,
}

impl LaunchAppearance {
    pub fn transparent(&self) -> bool {
        self.transparency && !self.low_power
    }
}

pub struct DesktopLaunch {
    pub appearance: Option<LaunchAppearance>,
    pub active_address: Mutex<Option<ServerAddress>>,
}

impl DesktopLaunch {
    pub fn from_settings(settings: &Result<DesktopSettings, SettingsError>) -> Self {
        Self {
            appearance: settings.as_ref().ok().map(|settings| LaunchAppearance {
                transparency: settings.transparency,
                low_power: settings.low_power,
            }),
            active_address: Mutex::new(settings.as_ref().ok().map(|s| s.server_address.clone())),
        }
    }

    pub fn transparent(&self) -> bool {
        self.appearance
            .is_some_and(|appearance| appearance.transparent())
    }
}

#[derive(Serialize)]
#[serde(rename_all = "camelCase")]
pub struct SettingsSnapshot {
    pub saved: DesktopSettings,
    pub launch: Option<LaunchAppearance>,
    pub active_address: Option<ServerAddress>,
}

#[derive(Clone, Copy, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum ConnectionMode {
    MainStartup,
    Restart,
}

#[tauri::command]
pub fn connect_desktop_server(
    app: tauri::AppHandle,
    window: tauri::WebviewWindow,
    owner: tauri::State<'_, SettingsOwner>,
    launch: tauri::State<'_, DesktopLaunch>,
    address: String,
    mode: ConnectionMode,
) -> Result<DesktopSettings, SettingsError> {
    if matches!(mode, ConnectionMode::MainStartup) && window.label() != "main" {
        return Err(SettingsError::Storage(
            "Only main-window startup can activate a connection.".into(),
        ));
    }
    owner.connect(&app, &launch, &address, mode)
}

#[tauri::command]
pub fn read_desktop_settings(
    app: tauri::AppHandle,
    owner: tauri::State<'_, SettingsOwner>,
    launch: tauri::State<'_, DesktopLaunch>,
) -> Result<SettingsSnapshot, SettingsError> {
    Ok(SettingsSnapshot {
        saved: owner.read(&app)?,
        launch: launch.appearance,
        active_address: launch
            .active_address
            .lock()
            .expect("address lock poisoned")
            .clone(),
    })
}

#[tauri::command]
pub fn desktop_launch_appearance(
    launch: tauri::State<'_, DesktopLaunch>,
) -> Option<LaunchAppearance> {
    launch.appearance
}

#[tauri::command]
pub fn change_desktop_setting(
    app: tauri::AppHandle,
    owner: tauri::State<'_, SettingsOwner>,
    change: SettingsChange,
) -> Result<DesktopSettings, SettingsError> {
    owner.change(&app, change)
}

#[derive(Clone, Debug, Serialize)]
#[serde(tag = "kind", content = "detail", rename_all = "camelCase")]
pub enum SettingsError {
    Storage(String),
    Address(AddressError),
}

impl From<tauri_plugin_store::Error> for SettingsError {
    fn from(error: tauri_plugin_store::Error) -> Self {
        Self::Storage(error.to_string())
    }
}

pub struct SettingsOwner {
    path: PathBuf,
    lock: Mutex<()>,
}

impl SettingsOwner {
    pub fn new(path: PathBuf) -> Self {
        Self {
            path,
            lock: Mutex::new(()),
        }
    }

    pub fn connect<R: tauri::Runtime>(
        &self,
        app: &tauri::AppHandle<R>,
        launch: &DesktopLaunch,
        address: &str,
        mode: ConnectionMode,
    ) -> Result<DesktopSettings, SettingsError> {
        let address = ServerAddress::parse(address).map_err(SettingsError::Address)?;
        let settings = self.change(app, SettingsChange::ServerAddress(address.clone()))?;
        if matches!(mode, ConnectionMode::MainStartup) {
            *launch.active_address.lock().expect("address lock poisoned") = Some(address);
        }
        Ok(settings)
    }

    pub fn read<R: tauri::Runtime>(
        &self,
        app: &tauri::AppHandle<R>,
    ) -> Result<DesktopSettings, SettingsError> {
        let _lock = self.lock.lock().expect("settings lock poisoned");
        self.load(app).map(|(_, settings)| settings)
    }

    fn load<R: tauri::Runtime>(
        &self,
        app: &tauri::AppHandle<R>,
    ) -> Result<
        (
            std::sync::Arc<tauri_plugin_store::Store<R>>,
            DesktopSettings,
        ),
        SettingsError,
    > {
        let store = app.store_builder(&self.path).disable_auto_save().build()?;
        let result = (|| {
            if let Err(error) = store.reload_ignore_defaults() {
                let recreate = match &error {
                    tauri_plugin_store::Error::Io(error) => {
                        error.kind() == std::io::ErrorKind::NotFound
                    }
                    tauri_plugin_store::Error::Deserialize(error) => {
                        error.downcast_ref::<serde_json::Error>().is_some()
                    }
                    _ => false,
                };
                if !recreate {
                    return Err(error.into());
                }
                store.clear();
                write_record(&store, &DesktopSettings::default())?;
                store.save()?;
            }
            let document = serde_json::Value::Object(store.entries().into_iter().collect());
            serde_json::from_value(document)
                .map_err(|error| SettingsError::Storage(error.to_string()))
        })();
        if result.is_err() {
            store.close_resource();
        }
        result.map(|settings| (store, settings))
    }

    pub fn change<R: tauri::Runtime>(
        &self,
        app: &tauri::AppHandle<R>,
        change: SettingsChange,
    ) -> Result<DesktopSettings, SettingsError> {
        let _lock = self.lock.lock().expect("settings lock poisoned");
        let (store, mut settings) = self.load(app)?;
        match change {
            SettingsChange::SelfUpdate(value) => settings.self_update = value,
            SettingsChange::ServerAddress(value) => settings.server_address = value,
            SettingsChange::Theme(value) => settings.theme = value,
            SettingsChange::ReducedMotion(value) => settings.reduced_motion = value,
            SettingsChange::Transparency(value) => settings.transparency = value,
            SettingsChange::LowPower(value) => settings.low_power = value,
            SettingsChange::Notifications(value) => settings.notifications = value,
            SettingsChange::ProjectOpening(value) => settings.project_opening = value,
        }
        let previous = store.entries();
        write_record(&store, &settings)?;
        if let Err(error) = store.save() {
            store.clear();
            for (key, value) in previous {
                store.set(key, value);
            }
            return Err(error.into());
        }
        app.emit("desktop-settings-changed", ())
            .map_err(|error| SettingsError::Storage(error.to_string()))?;
        Ok(settings)
    }
}

fn write_record<R: tauri::Runtime>(
    store: &tauri_plugin_store::Store<R>,
    settings: &DesktopSettings,
) -> Result<(), SettingsError> {
    let record = serde_json::to_value(settings)
        .map_err(|error| SettingsError::Storage(error.to_string()))?;
    let serde_json::Value::Object(fields) = record else {
        unreachable!("settings serialize as an object")
    };
    for (key, value) in fields {
        store.set(key, value);
    }
    Ok(())
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(try_from = "String", into = "String")]
pub struct ServerAddress(tauri::Url);

#[derive(Clone, Debug, PartialEq, Serialize)]
#[serde(rename_all = "camelCase")]
pub enum AddressError {
    InvalidUrl,
    UnsupportedScheme,
    MissingHost,
}

impl std::fmt::Display for AddressError {
    fn fmt(&self, f: &mut std::fmt::Formatter<'_>) -> std::fmt::Result {
        write!(f, "{self:?}")
    }
}

impl ServerAddress {
    pub fn parse(input: &str) -> Result<Self, AddressError> {
        let url = tauri::Url::parse(input.trim()).map_err(|_| AddressError::InvalidUrl)?;
        if !matches!(url.scheme(), "ws" | "wss") {
            return Err(AddressError::UnsupportedScheme);
        }
        if url.host().is_none() {
            return Err(AddressError::MissingHost);
        }
        Ok(Self(url))
    }

    pub fn as_str(&self) -> &str {
        self.0.as_str()
    }
}

impl Default for ServerAddress {
    fn default() -> Self {
        Self::parse("ws://127.0.0.1:53082/rpc").unwrap()
    }
}

impl TryFrom<String> for ServerAddress {
    type Error = AddressError;
    fn try_from(value: String) -> Result<Self, Self::Error> {
        Self::parse(&value)
    }
}

impl From<ServerAddress> for String {
    fn from(value: ServerAddress) -> Self {
        value.0.into()
    }
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum SelfUpdate {
    #[default]
    Enabled,
    Disabled,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum Theme {
    #[default]
    System,
    Light,
    Dark,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum Notifications {
    #[default]
    Automatic,
    Native,
    InApp,
}

#[derive(Clone, Debug, Default, PartialEq, Serialize, Deserialize)]
#[serde(rename_all = "camelCase")]
pub enum ProjectOpening {
    #[default]
    LastUsed,
    Tasks,
    Sessions,
}

#[derive(Clone, Debug, PartialEq, Serialize, Deserialize)]
#[serde(default, rename_all = "camelCase", remote = "Self")]
pub struct DesktopSettings {
    #[serde(deserialize_with = "current_version")]
    pub version: u32,
    #[serde(deserialize_with = "preference")]
    pub self_update: SelfUpdate,
    #[serde(deserialize_with = "preference")]
    pub server_address: ServerAddress,
    #[serde(deserialize_with = "preference")]
    pub theme: Theme,
    #[serde(deserialize_with = "preference")]
    pub reduced_motion: bool,
    #[serde(deserialize_with = "transparency")]
    pub transparency: bool,
    #[serde(deserialize_with = "preference")]
    pub low_power: bool,
    #[serde(deserialize_with = "preference")]
    pub notifications: Notifications,
    #[serde(deserialize_with = "preference")]
    pub project_opening: ProjectOpening,
}

impl Serialize for DesktopSettings {
    fn serialize<S: serde::Serializer>(&self, serializer: S) -> Result<S::Ok, S::Error> {
        Self::serialize(self, serializer)
    }
}

impl<'de> Deserialize<'de> for DesktopSettings {
    fn deserialize<D: serde::Deserializer<'de>>(deserializer: D) -> Result<Self, D::Error> {
        let document = serde_json::Map::<String, serde_json::Value>::deserialize(deserializer)?;
        Self::deserialize(serde_json::Value::Object(document)).map_err(serde::de::Error::custom)
    }
}

fn preference<'de, D: serde::Deserializer<'de>, T: serde::de::DeserializeOwned + Default>(
    deserializer: D,
) -> Result<T, D::Error> {
    let value = serde_json::Value::deserialize(deserializer)?;
    Ok(serde_json::from_value(value).unwrap_or_default())
}

fn transparency<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<bool, D::Error> {
    let value = serde_json::Value::deserialize(deserializer)?;
    Ok(serde_json::from_value(value).unwrap_or_else(|_| DesktopSettings::default().transparency))
}

fn current_version<'de, D: serde::Deserializer<'de>>(deserializer: D) -> Result<u32, D::Error> {
    serde::de::IgnoredAny::deserialize(deserializer)?;
    Ok(SETTINGS_VERSION)
}

impl Default for DesktopSettings {
    fn default() -> Self {
        Self {
            version: SETTINGS_VERSION,
            self_update: SelfUpdate::default(),
            server_address: ServerAddress::default(),
            theme: Theme::default(),
            reduced_motion: false,
            transparency: true,
            low_power: false,
            notifications: Notifications::default(),
            project_opening: ProjectOpening::default(),
        }
    }
}

#[derive(Clone, Debug, Deserialize)]
#[serde(tag = "field", content = "value", rename_all = "camelCase")]
pub enum SettingsChange {
    SelfUpdate(SelfUpdate),
    ServerAddress(ServerAddress),
    Theme(Theme),
    ReducedMotion(bool),
    Transparency(bool),
    LowPower(bool),
    Notifications(Notifications),
    ProjectOpening(ProjectOpening),
}

#[cfg(test)]
mod tests {
    use super::*;

    fn app() -> tauri::App<tauri::test::MockRuntime> {
        tauri::test::mock_builder()
            .plugin(tauri_plugin_store::Builder::new().build())
            .build(tauri::test::mock_context(tauri::test::noop_assets()))
            .unwrap()
    }

    #[test]
    fn recreates_missing_and_invalid_documents() {
        let app = app();
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("settings.json");
        let owner = SettingsOwner::new(path.clone());
        assert_eq!(
            owner.read(app.handle()).unwrap(),
            DesktopSettings::default()
        );
        assert!(path.is_file());
        std::fs::write(&path, b"[").unwrap();
        assert_eq!(
            owner.read(app.handle()).unwrap(),
            DesktopSettings::default()
        );
        let saved: DesktopSettings = serde_json::from_slice(&std::fs::read(path).unwrap()).unwrap();
        assert_eq!(saved, DesktopSettings::default());
    }

    #[test]
    fn does_not_recreate_unreadable_storage_or_leave_an_exit_writer() {
        let app = app();
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("settings.json");
        std::fs::create_dir(&path).unwrap();
        let owner = SettingsOwner::new(path.clone());
        assert!(owner.read(app.handle()).is_err());
        assert!(path.is_dir());
        assert!(app.get_store(&path).is_none());
    }

    #[cfg(unix)]
    #[test]
    fn permission_denied_read_preserves_the_document() {
        use std::os::unix::fs::PermissionsExt;
        let app = app();
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("settings.json");
        let original = br#"{"theme":"dark"}"#;
        std::fs::write(&path, original).unwrap();
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o0)).unwrap();
        let owner = SettingsOwner::new(path.clone());
        let result = owner.read(app.handle());
        std::fs::set_permissions(&path, std::fs::Permissions::from_mode(0o600)).unwrap();
        assert!(result.is_err());
        assert!(app.get_store(&path).is_none());
        assert_eq!(std::fs::read(&path).unwrap(), original);
    }

    #[test]
    fn changes_only_requested_preferences_and_preserves_applied_values_on_save_failure() {
        let app = app();
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("settings.json");
        std::fs::write(&path, br#"{"selfUpdate":"disabled","theme":"dark"}"#).unwrap();
        let owner = SettingsOwner::new(path.clone());
        let saved = owner
            .change(app.handle(), SettingsChange::ReducedMotion(true))
            .unwrap();
        assert_eq!(saved.self_update, SelfUpdate::Disabled);
        assert_eq!(saved.theme, Theme::Dark);
        assert!(saved.reduced_motion);
        let store = app.get_store(&path).unwrap();
        let previous_cache = store.entries();
        let mut permissions = std::fs::metadata(&path).unwrap().permissions();
        permissions.set_readonly(true);
        std::fs::set_permissions(&path, permissions).unwrap();
        assert!(owner
            .change(app.handle(), SettingsChange::Theme(Theme::Light))
            .is_err());
        let before: std::collections::HashMap<_, _> = previous_cache.into_iter().collect();
        let after: std::collections::HashMap<_, _> = store.entries().into_iter().collect();
        assert_eq!(before, after);
        assert_eq!(owner.read(app.handle()).unwrap(), saved);
    }

    #[test]
    fn failed_connection_save_does_not_activate_an_address() {
        let app = app();
        let dir = tempfile::tempdir().unwrap();
        let path = dir.path().join("settings.json");
        let owner = SettingsOwner::new(path.clone());
        let initial = owner.read(app.handle());
        let launch = DesktopLaunch::from_settings(&initial);
        let mut permissions = std::fs::metadata(&path).unwrap().permissions();
        permissions.set_readonly(true);
        std::fs::set_permissions(&path, permissions).unwrap();
        assert!(owner
            .connect(
                app.handle(),
                &launch,
                "wss://other.example/rpc",
                ConnectionMode::MainStartup
            )
            .is_err());
        assert_eq!(
            *launch.active_address.lock().unwrap(),
            Some(ServerAddress::default())
        );
    }

    #[test]
    fn startup_activates_saved_address_while_restart_only_changes_leave_it_active() {
        let app = app();
        let dir = tempfile::tempdir().unwrap();
        let owner = SettingsOwner::new(dir.path().join("settings.json"));
        let launch = DesktopLaunch::from_settings(&owner.read(app.handle()));
        let first = owner
            .connect(
                app.handle(),
                &launch,
                "wss://first.example/rpc",
                ConnectionMode::MainStartup,
            )
            .unwrap();
        assert_eq!(
            *launch.active_address.lock().unwrap(),
            Some(first.server_address.clone())
        );
        let second = owner
            .connect(
                app.handle(),
                &launch,
                "wss://second.example/rpc",
                ConnectionMode::Restart,
            )
            .unwrap();
        assert_eq!(
            *launch.active_address.lock().unwrap(),
            Some(first.server_address)
        );
        let restarted = DesktopLaunch::from_settings(&owner.read(app.handle()));
        assert_eq!(
            *restarted.active_address.lock().unwrap(),
            Some(second.server_address)
        );
    }

    #[test]
    fn preserves_self_update_when_adding_preferences() {
        let settings: DesktopSettings =
            serde_json::from_str(r#"{"version":1,"selfUpdate":"disabled"}"#).unwrap();
        assert_eq!(settings.self_update, SelfUpdate::Disabled);
        assert_eq!(settings.theme, Theme::System);
        assert!(!settings.low_power);
        assert!(settings.transparency);
        assert!(!settings.reduced_motion);
        assert_eq!(settings.notifications, Notifications::Automatic);
        assert_eq!(settings.project_opening, ProjectOpening::LastUsed);
        assert_eq!(settings.server_address.as_str(), "ws://127.0.0.1:53082/rpc");
    }

    #[test]
    fn defaults_unknown_fields_without_losing_valid_preferences() {
        let settings: DesktopSettings = serde_json::from_value(serde_json::json!({
            "version": 99, "selfUpdate": "disabled", "theme": "future",
            "notifications": "future", "projectOpening": null,
            "reducedMotion": true, "transparency": false, "unknown": 42
        }))
        .unwrap();
        assert_eq!(settings.version, 1);
        assert_eq!(settings.theme, Theme::System);
        assert_eq!(settings.notifications, Notifications::Automatic);
        assert_eq!(settings.project_opening, ProjectOpening::LastUsed);
        assert_eq!(settings.self_update, SelfUpdate::Disabled);
        assert!(settings.reduced_motion);
        assert!(!settings.transparency);
    }

    #[test]
    fn rejects_invalid_whole_documents() {
        for document in ["null", "[]", "true", "42", "\"settings\"", "{"] {
            assert!(
                serde_json::from_str::<DesktopSettings>(document).is_err(),
                "{document}"
            );
        }
    }

    #[test]
    fn accepts_websocket_addresses_and_rejects_invalid_changes() {
        for (input, expected, port) in [
            (" ws://localhost/rpc ", "ws://localhost/rpc", 80),
            (
                "wss://example.com:443/custom",
                "wss://example.com/custom",
                443,
            ),
            (
                "ws://[::1]:53082/custom?q=1",
                "ws://[::1]:53082/custom?q=1",
                53082,
            ),
        ] {
            let address = ServerAddress::parse(input).unwrap();
            assert_eq!(address.as_str(), expected);
            assert_eq!(address.0.port_or_known_default(), Some(port));
        }
        for input in ["", "http://localhost", "localhost:53082", "ws://"] {
            assert!(ServerAddress::parse(input).is_err());
        }
        assert!(serde_json::from_value::<SettingsChange>(
            serde_json::json!({"field": "theme", "value": "future"})
        )
        .is_err());
        assert!(serde_json::from_value::<SettingsChange>(
            serde_json::json!({"field": "serverAddress", "value": "http://localhost"})
        )
        .is_err());
    }
}
