import { create } from "@app/server-api-contract";
import {
  ChatSettingsService,
  SettingsSnapshotSchema,
} from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";
import { StreamCompletionSchema } from "@app/server-api-contract/gen/kent/api/shared/foundation_pb";
import { requireChatSuccess } from "./chatErrors";
import { settings, sessionFacts } from "./chatSettings";
import { required } from "./chatWire";
import { requireChatSessionID } from "./chatTarget";
import { streamCompletionFailure } from "./protobufRpc";
import { subscriptionStream } from "./subscriptionStream";
import type { ChatSettingsObservation, ChatSessionTarget } from "./chatTypes";
import type { RpcTransport } from "./transport";

export function chatSettingsObservation(
  transport: RpcTransport,
  target: ChatSessionTarget,
  reportOverflow: () => Promise<void>,
) {
  return subscriptionStream<ChatSettingsObservation>((emit) => {
    const sessionID = requireChatSessionID(target);
    const method = ChatSettingsService.method.subscribe;
    return transport.subscribeDescriptor({
      method,
      request: create(method.input, { sessionId: sessionID }),
      attachment: { projectID: target.projectID, sessionID },
      eventDescriptor: SettingsSnapshotSchema,
      completionDescriptor: StreamCompletionSchema,
      onStart: (response) => {
        requireChatSuccess(method, response);
      },
      handler: {
        onEvent: (value) => {
          emit({
            kind: "snapshot",
            snapshot: {
              sessionName: value.sessionName ?? null,
              settings: settings(required(value.settings)),
              session: sessionFacts(required(value.session), sessionID),
            },
          });
        },
        onError: (error) => {
          emit({ kind: "error", error });
        },
        onComplete: (completion) => {
          const failure = streamCompletionFailure(completion);
          if (failure !== undefined) emit({ kind: "error", error: failure });
          return failure;
        },
      },
    });
  }, reportOverflow);
}
