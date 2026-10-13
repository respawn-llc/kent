import { create } from "@app/server-api-contract";
import * as T from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import { ConversationFreshness } from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";
import { ApiClient } from "@/api/composition";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";

export async function readWireUser(sourceID: string) {
  const sessionID = "123e4567-e89b-42d3-a456-426614174000";
  const client = new ApiClient(
    new FakeRpcTransport([
      {
        descriptor: T.ReadService.method.getPage,
        result: create(T.ReadService.method.getPage.output, {
          outcome: {
            case: "success",
            value: {
              transcript: {
                sessionId: sessionID,
                conversationFreshness: ConversationFreshness.ESTABLISHED,
                entries: [
                  {
                    visibility: T.EntryVisibility.ONGOING,
                    integrity: T.RowIntegrity.VALID,
                    locator: { eventSequence: 8n, rowOrdinal: 1 },
                    row: { case: "user", value: { text: "Sent message", queueItemIds: [sourceID] } },
                  },
                ],
              },
            },
          },
        }),
      },
    ]),
    unexpectedProjectOverflow,
  );
  const result = await client.chat.getTranscriptPage(
    { projectID: "project-1", sessionID },
    { direction: "older", value: 41 },
  );
  const entry = result.entries[0];
  if (entry === undefined) throw new Error("Transcript fixture is empty.");
  return entry;
}
