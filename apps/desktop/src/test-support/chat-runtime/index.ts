import { vi, type Mock } from "vitest";
import type {
  ChatMainViewRead,
  ChatTranscriptPage,
  ChatTranscriptPayloadByKind,
  ChatTranscriptHandler,
} from "@/api";
import type { ChatRuntimeApi, ChatRuntimeHost } from "@/app-facade";
import { createFixtureChat, hydration, mainViewRead, transcriptPage } from "@/dev-showcase/fixtures";
import { row } from "@/test-support/transcript-window";

export {
  target,
  mainViewRead,
  hydration,
  hydrationWithCursor,
  seededProjection,
  runtimeUpdate,
  runtimeUnavailablePayload,
  changedIdentity,
  goalStatus,
  worktreeOutcome,
  transcriptPage,
  deferred,
  requireValue,
} from "@/dev-showcase/fixtures";

export function incompatibleHydration(
  sessionName: string,
  text: string,
): ChatTranscriptPayloadByKind["hydration"] {
  const payload = hydration();
  return {
    ...payload,
    SessionIdentity: { ...payload.SessionIdentity, SessionName: sessionName },
    TailSegment: { Entries: [{ ...row(10), User: { Text: text } }], OlderCursor: null, HasMoreAbove: false },
  };
}

export function runtimeHost(effects: Omit<ChatRuntimeHost, "logger"> = {}): ChatRuntimeHost {
  return { logger: { append: vi.fn().mockResolvedValue(undefined) }, ...effects };
}

export function runtimeApi({
  reads = [Promise.resolve(mainViewRead())],
  pages = [Promise.resolve(transcriptPage(null))],
}: Readonly<{
  reads?: readonly Promise<ChatMainViewRead>[];
  pages?: readonly Promise<ChatTranscriptPage>[];
}> = {}): Readonly<{
  api: ChatRuntimeApi;
  getMainView: Mock<ChatRuntimeApi["getMainView"]>;
  getTranscriptPage: Mock<ChatRuntimeApi["getTranscriptPage"]>;
  subscribeTranscript: Mock<ChatRuntimeApi["subscribeTranscript"]>;
  handlers: readonly ChatTranscriptHandler[];
}> {
  let readIndex = 0;
  let pageIndex = 0;
  const getMainView = vi.fn(
    async () => reads[readIndex++] ?? Promise.reject(new Error("Unexpected Main View call.")),
  );
  const getTranscriptPage = vi.fn(
    async () => pages[pageIndex++] ?? Promise.reject(new Error("Unexpected transcript page call.")),
  );
  const runtime = createFixtureChat(getMainView, getTranscriptPage);
  const subscribeTranscript = vi.fn<ChatRuntimeApi["subscribeTranscript"]>((...args) => {
    const subscription = runtime.api.subscribeTranscript(...args);
    return { close: vi.fn(subscription.close) };
  });
  return {
    api: { getMainView, getTranscriptPage, subscribeTranscript },
    getMainView,
    getTranscriptPage,
    subscribeTranscript,
    handlers: runtime.handlers,
  };
}
