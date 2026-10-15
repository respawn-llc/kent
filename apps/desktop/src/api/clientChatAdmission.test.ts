import { describe, expect, it } from "bun:test";
import { fromJson } from "@bufbuild/protobuf";
import { create } from "@app/server-api-contract";
import {
  ChatService,
  InputAcceptedSchema,
  InputAdmissionDisposition,
} from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { parsePendingWorkItemID } from "./pendingWork";

const sessionID = "123e4567-e89b-42d3-a456-426614174000";
const queueItemID = parsePendingWorkItemID("223e4567-e89b-42d3-a456-426614174000");
const target = { kind: "session", projectID: "project-1", sessionID } as const;

async function admit(disposition: number) {
  const accepted = fromJson(InputAcceptedSchema, {
    queueItem: { id: queueItemID.toJSONValue() },
    disposition,
  });
  const transport = new FakeRpcTransport([
    {
      descriptor: ChatService.method.steer,
      result: create(ChatService.method.steer.output, {
        outcome: {
          case: "success",
          value: {
            session: { sessionId: sessionID },
            outcome: {
              case: "accepted",
              value: accepted,
            },
          },
        },
      }),
    },
  ]);
  return new ApiClient(transport, unexpectedProjectOverflow).chat.steer(
    target,
    { kind: "text", text: "continue" },
    queueItemID,
  );
}

describe("Desktop Chat admission disposition", () => {
  it.each([
    [InputAdmissionDisposition.STARTED_TURN, "started_turn"],
    [InputAdmissionDisposition.PENDING_STEER, "pending_steer"],
    [InputAdmissionDisposition.PENDING_QUEUE, "pending_queue"],
  ] as const)("decodes %s as %s", async (wireDisposition, disposition) => {
    const result = await admit(wireDisposition);
    expect(result.outcome).toMatchObject({ kind: "accepted", disposition, queueItemID });
  });

  it.each([InputAdmissionDisposition.UNSPECIFIED, 99])(
    "rejects missing or unknown disposition %s",
    async (disposition) => {
      const rejected = await admit(disposition).then(
        () => {
          throw new Error("Expected an unknown disposition to be rejected.");
        },
        (error: unknown) => error,
      );
      expect(rejected).toBeInstanceOf(Error);
    },
  );
});
