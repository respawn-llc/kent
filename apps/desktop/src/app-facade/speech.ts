import type { NativeSpeechOriginal, NativeMicrophoneEvent } from "@app/native-bridge";
import type { SpeechAvailable, SpeechEncoding, SpeechResult } from "@/api";

export type SpeechSource =
  | Readonly<{ kind: "native"; original: NativeSpeechOriginal; durationMs: number }>
  | Readonly<{ kind: "browser"; blob: Blob; durationMs: number }>;

export type SpeechRequest = Readonly<{
  result(): Promise<SpeechResult>;
  cancel(): Promise<void>;
}>;

export type SpeechCapture = SpeechRequest &
  Readonly<{
    microphone?: Readonly<{
      start(receive: (event: NativeMicrophoneEvent) => void): Promise<void>;
      stop(): Promise<void>;
    }>;
    append(chunk: Blob): Promise<Readonly<{ bytes: number; limitReached: boolean }>>;
    finalize(durationMs: number): Promise<SpeechSource>;
  }>;

export type SpeechTransfer = Readonly<{
  microphone: "native" | "browser";
  begin(available: SpeechAvailable, encoding: SpeechEncoding): Promise<SpeechCapture>;
  retry(source: SpeechSource, available: SpeechAvailable): Promise<SpeechRequest>;
  discard(source: SpeechSource): Promise<void>;
  reveal(source: SpeechSource): Promise<void>;
  saveCopy(source: SpeechSource, title: string): Promise<void>;
  limitSound(): Promise<boolean>;
}>;

export type { NativeMicrophoneEvent as SpeechMicrophoneEvent } from "@app/native-bridge";

export function speechSourceBytes(source: SpeechSource): number {
  return source.kind === "native" ? source.original.bytes : source.blob.size;
}
