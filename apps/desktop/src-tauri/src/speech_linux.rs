use tauri::Manager;
use webkit2gtk::glib::prelude::*;
use webkit2gtk::{PermissionRequestExt, SettingsExt, UserMediaPermissionRequestExt, WebViewExt};

pub fn init() -> tauri::plugin::TauriPlugin<tauri::Wry> {
    tauri::plugin::Builder::new("speech-capture")
        .on_webview_ready(|webview| {
            let trusted = if tauri::is_dev() {
                webview.config().build.dev_url.clone()
            } else {
                Some(tauri::Url::parse("tauri://localhost").expect("valid application URL"))
            };
            if let Err(error) = webview.with_webview(move |platform| {
                let view = platform.inner();
                let Some(settings) = view.settings() else {
                    eprintln!("Speech capture initialization failed: WebView settings unavailable");
                    return;
                };
                settings.set_enable_media_stream(true);
                view.connect_permission_request(move |view, request| {
                    let Some(media) =
                        request.downcast_ref::<webkit2gtk::UserMediaPermissionRequest>()
                    else {
                        return false;
                    };
                    let origin_matches = view
                        .uri()
                        .and_then(|uri| tauri::Url::parse(&uri).ok())
                        .zip(trusted.as_ref())
                        .is_some_and(|(url, trusted)| {
                            url.scheme() == trusted.scheme()
                                && url.host_str() == trusted.host_str()
                                && url.port_or_known_default() == trusted.port_or_known_default()
                        });
                    if origin_matches && media.is_for_audio_device() && !media.is_for_video_device()
                    {
                        request.allow();
                    } else {
                        request.deny();
                    }
                    true
                });
            }) {
                eprintln!("Speech capture initialization failed: {error}");
            }
        })
        .build()
}
