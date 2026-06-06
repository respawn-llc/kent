export type RpcErrorInfo = Readonly<{
  code: number;
  message: string;
  method: string;
}>;

export class RpcError extends Error {
  readonly code: number;
  readonly method: string;

  constructor(info: RpcErrorInfo) {
    super(info.message);
    this.name = "RpcError";
    this.code = info.code;
    this.method = info.method;
  }
}

export class TransportError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "TransportError";
  }
}

export class ContractError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ContractError";
  }
}

export class ProtocolMismatchError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "ProtocolMismatchError";
  }
}

export class StartupConfigurationError extends Error {
  constructor(message: string) {
    super(message);
    this.name = "StartupConfigurationError";
  }
}

export function errorMessage(error: unknown): string {
  if (typeof error === "string") {
    return normalizeMessage(error);
  }
  if (error instanceof Error) {
    return normalizeMessage(error.message);
  }
  if (hasStringMessage(error)) {
    return normalizeMessage(error.message);
  }
  if (isObject(error)) {
    try {
      return normalizeMessage(JSON.stringify(error));
    } catch {
      return "Unknown error";
    }
  }
  return "Unknown error";
}

function normalizeMessage(message: string): string {
  const trimmed = message.trim();
  return trimmed.length > 0 ? trimmed : "Unknown error";
}

function hasStringMessage(error: unknown): error is Readonly<{ message: string }> {
  return isObject(error) && typeof error.message === "string";
}

function isObject(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null;
}
