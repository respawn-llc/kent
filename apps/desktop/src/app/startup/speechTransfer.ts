import type { NativeBridge } from "@app/native-bridge";
import { SpeechOperationError, SpeechTransportError, type SpeechResult } from "@/api";
import { assertSpeechSize, decodeSpeechResult, speechUploadURL, uploadSpeechBlob } from "@/api/composition";
import { speechSourceBytes, type SpeechRequest, type SpeechSource, type SpeechTransfer } from "@/app-facade";

export function createSpeechTransfer(bridge: NativeBridge, endpoint: string): SpeechTransfer {
  const native = bridge.speech;
  const nativeRequest = (handle: number): SpeechRequest => ({
    result: async () => {
      if (native === null) throw new Error("Native speech is unavailable.");
      const response = await native.finish(handle);
      if (response.kind === "interrupted") throw new SpeechTransportError(response.diagnostic);
      return decodeSpeechResult(response.status, Uint8Array.from(response.body));
    },
    cancel: async () => {
      await native?.cancel(handle);
    },
  });
  const download = (source: Extract<SpeechSource, { kind: "browser" }>) => {
    const url = URL.createObjectURL(source.blob);
    const anchor = document.createElement("a");
    anchor.href = url;
    anchor.download = "recording";
    anchor.click();
    URL.revokeObjectURL(url);
  };
  return {
    begin: async (available, encoding) => {
      const url = speechUploadURL(endpoint, available.uploadUrl);
      if (native !== null) {
        const recording = await native.create({ ...encoding, maxBytes: available.maxBytes, uploadUrl: url });
        return {
          ...nativeRequest(recording.handle),
          append: async (chunk) => native.append(recording.handle, new Uint8Array(await chunk.arrayBuffer())),
          finalize: async (durationMs) => ({
            kind: "native",
            original: await native.finalize(recording.handle),
            durationMs,
          }),
        };
      }
      const chunks: Blob[] = [];
      let bytes = 0;
      let source: Blob | null = null;
      const abort = new AbortController();
      return {
        append: async (chunk) => {
          chunks.push(chunk);
          bytes += chunk.size;
          return { bytes, limitReached: bytes >= available.maxBytes };
        },
        finalize: async (durationMs) => {
          source = new Blob(chunks, { type: encoding.mimeType });
          chunks.length = 0;
          return { kind: "browser", blob: source, durationMs };
        },
        result: async (): Promise<SpeechResult> => {
          if (source === null) throw new Error("Recording must be finalized before upload.");
          assertSpeechSize(source.size, available);
          return uploadSpeechBlob(url, source, abort.signal);
        },
        cancel: async () => {
          abort.abort();
        },
      };
    },
    retry: async (source, available) => {
      assertSpeechSize(speechSourceBytes(source), available);
      const url = speechUploadURL(endpoint, available.uploadUrl);
      if (source.kind === "native") {
        if (native === null) throw new Error("Native speech is unavailable.");
        return nativeRequest(await native.uploadOriginal(source.original, url, available.maxBytes));
      }
      const abort = new AbortController();
      return {
        result: async () => uploadSpeechBlob(url, source.blob, abort.signal),
        cancel: async () => {
          abort.abort();
        },
      };
    },
    discard: async (source) => {
      if (source.kind === "native") {
        if (native === null) throw new Error("Native speech is unavailable.");
        await native.discard(source.original.path);
      }
    },
    reveal: async (source) => {
      if (source.kind === "browser") download(source);
      else await bridge.files.openFile({ path: source.original.path });
    },
    saveCopy: async (source, title) => {
      if (source.kind === "browser") download(source);
      else {
        if (native === null) throw new Error("Native speech is unavailable.");
        const outcome = await native.saveCopy(source.original.path, title);
        if (outcome.kind === "original-path") throw new SpeechOperationError("originalPath");
      }
    },
    limitSound: async () => native?.limitSound() ?? false,
  };
}
