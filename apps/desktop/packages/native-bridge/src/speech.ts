import { invoke } from "@tauri-apps/api/core";

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
  create(options: NativeSpeechOptions): Promise<NativeSpeechCapture>;
  append(handle: number, bytes: Uint8Array): Promise<Readonly<{ bytes: number; limitReached: boolean }>>;
  finalize(handle: number): Promise<NativeSpeechOriginal>;
  finish(handle: number): Promise<NativeSpeechReply>;
  cancel(handle: number): Promise<void>;
  uploadOriginal(original: NativeSpeechOriginal, uploadUrl: string, maxBytes: number): Promise<number>;
  discard(path: string): Promise<void>;
  saveCopy(path: string, title: string): Promise<string | null>;
  limitSound(): Promise<boolean>;
}>;

export function createTauriSpeechBridge(): NativeSpeechBridge {
  return {
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
