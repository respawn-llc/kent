import { Channel, invoke } from "@tauri-apps/api/core";

export type NativeMicrophoneEvent =
  | Readonly<{ kind: "ready" | "limit" }>
  | Readonly<{ kind: "meter"; power: number }>
  | Readonly<{ kind: "finished"; error: string | null }>;

export type NativeSpeechOriginal = Readonly<{
  path: string;
  mimeType: string;
  bytes: number;
}>;

export type NativeSpeechCapture = Readonly<{ handle: number; path: string }>;
export type NativeSpeechOptions = Readonly<{
  mimeType: string;
  extension: string;
  maxBytes: number;
  uploadUrl: string;
}>;
export type NativeSpeechReply =
  | Readonly<{ kind: "response"; status: number; body: readonly number[] }>
  | Readonly<{ kind: "interrupted"; diagnostic: string }>;
export type NativeSpeechBridge = Readonly<{
  startMicrophone(handle: number, receive: (event: NativeMicrophoneEvent) => void): Promise<void>;
  stopMicrophone(handle: number): Promise<void>;
  create(options: NativeSpeechOptions): Promise<NativeSpeechCapture>;
  append(handle: number, bytes: Uint8Array): Promise<Readonly<{ bytes: number; limitReached: boolean }>>;
  finalize(handle: number): Promise<NativeSpeechOriginal>;
  finish(handle: number): Promise<NativeSpeechReply>;
  cancel(handle: number): Promise<void>;
  uploadOriginal(original: NativeSpeechOriginal, uploadUrl: string, maxBytes: number): Promise<number>;
  discard(path: string): Promise<void>;
  saveCopy(path: string, title: string): Promise<Readonly<{ kind: "saved" | "canceled" | "original-path" }>>;
  limitSound(): Promise<boolean>;
}>;

export function createTauriSpeechBridge(): NativeSpeechBridge {
  return {
    startMicrophone: async (handle, receive) => {
      const events = new Channel<NativeMicrophoneEvent>();
      events.onmessage = receive;
      await invoke("speech_start_microphone", { handle, events });
    },
    stopMicrophone: async (handle) => invoke("speech_stop_microphone", { handle }),
    create: async (options) => invoke("speech_create", { options }),
    append: async (handle, bytes) => invoke("speech_append", { handle, bytes: Array.from(bytes) }),
    finalize: async (handle) => invoke("speech_finalize", { handle }),
    finish: async (handle) => invoke("speech_finish", { handle }),
    cancel: async (handle) => invoke("speech_cancel", { handle }),
    uploadOriginal: async (original, uploadUrl, maxBytes) =>
      invoke("speech_upload_original", { original, uploadUrl, maxBytes }),
    discard: async (path) => invoke("speech_discard", { path }),
    saveCopy: async (path, title) => invoke("speech_save_copy", { path, title }),
    limitSound: async () => invoke("speech_limit_sound"),
  };
}
