import type { ChatTranscriptMessage, ChatTranscriptPage, ChatTranscriptPayloadByKind } from "@/api";

import type { ChatTranscriptHydrationKind } from "./chatTranscriptObservation";

export type ChatTranscriptSink = Readonly<{
  openingStarted(): void;
  openingSucceeded(page: ChatTranscriptPage): void;
  openingFailed(error: Error): void;
  hydration(kind: ChatTranscriptHydrationKind, hydration: ChatTranscriptPayloadByKind["hydration"]): void;
  event(event: Exclude<ChatTranscriptMessage, { kind: "hydration" }>): void;
  recoveryStarted(): void;
  dispose(): void;
}>;
