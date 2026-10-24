import type { TFunction } from "i18next";
import { errorMessage, SpeechFailure, SpeechOperationError, SpeechTransportError } from "@/api";
import { SpeechActionError } from "./capture";

export function speechFailureMessage(error: unknown, t: TFunction): string {
  if (error instanceof SpeechActionError) return speechFailureMessage(error.cause, t);
  if (error instanceof DOMException) {
    if (error.name === "NotAllowedError") return t("speech.errors.permission");
    if (error.name === "NotFoundError" || error.name === "NotReadableError") return t("speech.errors.device");
  }
  if (error instanceof SpeechOperationError) return t(`speech.errors.${error.reason}`);
  if (error instanceof SpeechTransportError) return `${t("speech.errors.transfer")}\n${error.message}`;
  if (error instanceof SpeechFailure) {
    const connection =
      error.connectionID === null ? "" : `${t("speech.connection", { connection: error.connectionID })}\n`;
    const guidance =
      error.httpStatus === null ? "" : `${t("speech.providerRejected", { status: error.httpStatus })}\n`;
    return `${connection}${guidance}${error.message}`;
  }
  return errorMessage(error);
}
