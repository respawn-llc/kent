import type { TFunction } from "i18next";
import { errorMessage, SpeechFailure, SpeechOperationError, SpeechTransportError } from "@/api";
import { SpeechActionError } from "./capture";

export function speechFailureMessage(error: unknown, t: TFunction): string {
  if (error instanceof SpeechActionError) return speechFailureMessage(error.cause, t);
  if (error instanceof SpeechOperationError) return t(`speech.errors.${error.reason}`);
  if (error instanceof SpeechTransportError) return `${t("speech.errors.transfer")}\n${error.message}`;
  if (error instanceof SpeechFailure && error.connectionID !== null)
    return `${t("speech.connection", { connection: error.connectionID })}\n${error.message}`;
  return errorMessage(error);
}
