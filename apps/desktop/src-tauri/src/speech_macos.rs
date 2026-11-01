use super::{append, owned, SpeechState};
use std::ffi::{c_char, c_void, CStr};
use std::sync::Mutex;
use tauri::{ipc::Channel, AppHandle, Manager, State, WebviewWindow};

#[derive(Clone, serde::Serialize)]
#[serde(tag = "kind", rename_all = "camelCase")]
pub enum Event {
    Ready,
    Meter { power: f64 },
    Limit,
    Finished { error: Option<String> },
}

#[repr(C)]
struct Callbacks {
    ready: extern "C" fn(*mut c_void),
    meter: extern "C" fn(*mut c_void, f64),
    segment: extern "C" fn(*mut c_void, *const u8, usize),
    finished: extern "C" fn(*mut c_void, *const c_char),
}

unsafe extern "C" {
    fn kent_speech_start(context: *mut c_void, callbacks: Callbacks) -> *mut c_void;
    fn kent_speech_stop(capture: *mut c_void);
}

pub struct Microphone(*mut c_void);
// The Objective-C instance is exclusively operated on its own serial queue.
// Moving the retained reference between Rust threads does not access that state.
unsafe impl Send for Microphone {}

impl Drop for Microphone {
    fn drop(&mut self) {
        unsafe { kent_speech_stop(self.0) };
    }
}

struct Context {
    app: AppHandle,
    handle: u64,
    events: Channel<Event>,
    failure: Mutex<Option<String>>,
}

impl Context {
    fn emit(&self, event: Event) {
        if let Err(error) = self.events.send(event) {
            eprintln!("Microphone observation delivery failed: {error}");
        }
    }
}

fn context<'a>(pointer: *mut c_void) -> &'a Context {
    // Native callbacks retain the context until their terminal finished callback.
    unsafe { &*pointer.cast::<Context>() }
}

extern "C" fn ready(pointer: *mut c_void) {
    context(pointer).emit(Event::Ready);
}

extern "C" fn meter(pointer: *mut c_void, power: f64) {
    context(pointer).emit(Event::Meter { power });
}

extern "C" fn segment(pointer: *mut c_void, bytes: *const u8, count: usize) {
    let context = context(pointer);
    let state = context.app.state::<SpeechState>();
    let result = state
        .recordings
        .lock()
        .map_err(|e| e.to_string())
        .and_then(|mut recordings| {
            let recording = recordings
                .get_mut(&context.handle)
                .ok_or("Recording is no longer open")?;
            append(recording, unsafe {
                std::slice::from_raw_parts(bytes, count)
            })
        });
    match result {
        Ok(written) if written.limit_reached => {
            context.emit(Event::Limit);
            stop(context);
        }
        Ok(_) => {}
        Err(error) => {
            *context
                .failure
                .lock()
                .expect("microphone failure lock poisoned") = Some(error);
            stop(context);
        }
    }
}

fn stop(context: &Context) {
    let state = context.app.state::<SpeechState>();
    let microphone = state
        .recordings
        .lock()
        .expect("speech lock poisoned")
        .get_mut(&context.handle)
        .and_then(|recording| recording.microphone.take());
    drop(microphone);
}

extern "C" fn finished(pointer: *mut c_void, error: *const c_char) {
    let context = unsafe { Box::from_raw(pointer.cast::<Context>()) };
    let native_error = if error.is_null() {
        None
    } else {
        Some(
            unsafe { CStr::from_ptr(error) }
                .to_string_lossy()
                .into_owned(),
        )
    };
    let error = context
        .failure
        .lock()
        .expect("microphone failure lock poisoned")
        .take()
        .or(native_error);
    context.emit(Event::Finished { error });
}

#[tauri::command]
pub async fn speech_start_microphone(
    app: AppHandle,
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    handle: u64,
    events: Channel<Event>,
) -> Result<(), String> {
    let mut recordings = state.recordings.lock().map_err(|e| e.to_string())?;
    let recording = owned(&mut recordings, handle, &window)?;
    if recording.microphone.is_some() || recording.file.is_none() {
        return Err("Recording cannot start a microphone in its current state".into());
    }
    let context = Box::into_raw(Box::new(Context {
        app,
        handle,
        events,
        failure: Mutex::new(None),
    }))
    .cast();
    let microphone = unsafe {
        kent_speech_start(
            context,
            Callbacks {
                ready,
                meter,
                segment,
                finished,
            },
        )
    };
    recording.microphone = Some(Microphone(microphone));
    Ok(())
}

#[tauri::command]
pub async fn speech_stop_microphone(
    window: WebviewWindow,
    state: State<'_, SpeechState>,
    handle: u64,
) -> Result<(), String> {
    let microphone = {
        let mut recordings = state.recordings.lock().map_err(|e| e.to_string())?;
        owned(&mut recordings, handle, &window)?.microphone.take()
    };
    drop(microphone);
    Ok(())
}
