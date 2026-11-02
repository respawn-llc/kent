export class ChatTranscriptFailure {
  readonly _tag = "ChatTranscriptFailure";
  readonly error: Error;
  constructor(cause: unknown) {
    this.error = cause instanceof Error ? cause : new Error("Transcript operation failed.", { cause });
  }
}
