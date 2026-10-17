import { create, decode } from "@app/server-api-contract";
import {
  FailureKind,
  SpeechService,
  UploadResultSchema,
  type Available,
  type Encoding,
} from "@app/server-api-contract/gen/kent/api/speech/speech_pb";
import { ContractError } from "./errors";
import { requireUnarySuccess } from "./protobufRpc";
import type { RpcTransport } from "./transport";

export type SpeechAvailable = Readonly<
  Pick<Available, "uploadUrl" | "maxBytes"> & {
    maxDurationSeconds: number;
    encodings: readonly SpeechEncoding[];
  }
>;
export type SpeechEncoding = Readonly<Pick<Encoding, "mimeType" | "extension">>;
export type SpeechResult = Readonly<{ kind: "transcript"; text: string }> | Readonly<{ kind: "noop" }>;
export type SpeechFailureKind = "unavailable" | "input" | "size-limit" | "connection" | "provider";

export class SpeechOperationError extends Error {
  constructor(readonly reason: "format" | "size" | "closed" | "unavailable" | "recording" | "originalPath") {
    super(reason);
  }
}

export class SpeechFailure extends Error {
  constructor(
    readonly kind: SpeechFailureKind,
    message: string,
    readonly connectionID: string | null = null,
    readonly httpStatus: number | null = null,
  ) {
    super(message);
    this.name = "SpeechFailure";
  }
}

export class SpeechTransportError extends Error {
  constructor(message: string, options?: ErrorOptions) {
    super(message, options);
    this.name = "SpeechTransportError";
  }
}

export async function speechAvailability(transport: RpcTransport): Promise<SpeechAvailable | null> {
  const method = SpeechService.method.getAvailability;
  const result = requireUnarySuccess(method, await transport.callDescriptor(method, create(method.input)));
  switch (result.state.case) {
    case "available": {
      const value = result.state.value;
      if (value.maxDuration === undefined) throw new ContractError("Speech duration is required.");
      return {
        uploadUrl: value.uploadUrl,
        maxBytes: value.maxBytes,
        encodings: value.encodings,
        maxDurationSeconds: Number(value.maxDuration.seconds) + value.maxDuration.nanos / 1e9,
      };
    }
    case "unavailable":
      return null;
    case undefined:
      throw new ContractError("Speech availability has no state.");
  }
}

export function decodeSpeechResult(status: number, body: Uint8Array): SpeechResult {
  const result = decode(UploadResultSchema, body);
  switch (result.outcome.case) {
    case "failure": {
      const failure = result.outcome.value;
      const kinds: Partial<Record<FailureKind, SpeechFailureKind>> = {
        [FailureKind.UNAVAILABLE]: "unavailable",
        [FailureKind.INPUT]: "input",
        [FailureKind.SIZE_LIMIT]: "size-limit",
        [FailureKind.CONNECTION]: "connection",
        [FailureKind.PROVIDER]: "provider",
      };
      const kind = kinds[failure.kind];
      if (kind === undefined) throw new ContractError("Unknown speech failure kind.");
      throw new SpeechFailure(
        kind,
        failure.diagnostic,
        failure.connectionId ?? null,
        failure.httpStatus ?? null,
      );
    }
    case "transcript":
      if (status < 200 || status >= 300) throw new ContractError("Failed upload returned a transcript.");
      return { kind: "transcript", text: result.outcome.value };
    case "noOp":
      if (status < 200 || status >= 300) throw new ContractError("Failed upload returned success.");
      return { kind: "noop" };
    case undefined:
      throw new ContractError("Speech response has no outcome.");
  }
}

export function speechUploadURL(endpoint: string, relative: string): string {
  const base = new URL(endpoint);
  if (base.protocol === "ws:") base.protocol = "http:";
  else if (base.protocol === "wss:") base.protocol = "https:";
  if (base.protocol !== "http:" && base.protocol !== "https:") {
    throw new ContractError("Speech requires an HTTP server endpoint.");
  }
  const url = new URL(relative, base);
  if (!relative.startsWith("/") || relative.startsWith("//") || url.origin !== base.origin) {
    throw new ContractError("Speech upload URL must be relative to the connected server.");
  }
  return url.href;
}

export function assertSpeechSize(bytes: number, available: SpeechAvailable): void {
  if (bytes > available.maxBytes) {
    throw new SpeechOperationError("size");
  }
}

export async function uploadSpeechBlob(url: string, blob: Blob, signal: AbortSignal): Promise<SpeechResult> {
  let response: Response;
  let body: ArrayBuffer;
  try {
    response = await fetch(url, {
      method: "POST",
      credentials: "omit",
      headers: { "Content-Type": blob.type },
      body: blob,
      signal,
    });
    body = await response.arrayBuffer();
  } catch (cause) {
    throw new SpeechTransportError(cause instanceof Error ? cause.message : String(cause), { cause });
  }
  return decodeSpeechResult(response.status, new Uint8Array(body));
}
