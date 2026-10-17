import { expect, it } from "bun:test";
import { create } from "@app/server-api-contract";
import { PromptCommandService } from "@app/server-api-contract/gen/kent/api/prompt_command/prompt_command_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { ChatService, InputAdmissionDisposition } from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import { ContractError } from "./errors";
import { parsePendingWorkItemID } from "./pendingWork";

it("reads command names and previews from the selected workspace or exact Session", async () => {
  const method = PromptCommandService.method.getCatalog;
  const commands = [{ name: "prompt:file", preview: "server preview" }];
  const transport = new FakeRpcTransport([
    {
      descriptor: method,
      result: create(method.output, { outcome: { case: "success", value: { commands } } }),
    },
  ]);
  const api = new ApiClient(transport, unexpectedProjectOverflow).chat;
  const projectID = "project-1";
  expect(
    await api.getCommandCatalog({ kind: "new_chat", projectID, workspace: { workspaceID: "workspace-1" } }),
  ).toEqual(commands);
  expect(transport.attachedProjectDescriptorCalls).toHaveLength(1);
  const sessionID = "123e4567-e89b-42d3-a456-426614174000";
  expect(await api.getCommandCatalog({ kind: "session", projectID, sessionID })).toEqual(commands);
  expect(transport.attachedSessionCalls[0]).toMatchObject({ sessionID });
  expect(transport.descriptorCalls.at(-1)?.request).toMatchObject({ sessionId: sessionID });
});

it("preserves typed catalog failure for the localized Retry surface", async () => {
  const method = PromptCommandService.method.getCatalog;
  const transport = new FakeRpcTransport([
    {
      descriptor: method,
      result: create(method.output, {
        outcome: {
          case: "error",
          value: { code: "catalog_read", detail: { case: "catalogRead", value: {} } },
        },
      }),
    },
  ]);
  expect(
    new ApiClient(transport, unexpectedProjectOverflow).chat.getCommandCatalog({
      kind: "new_chat",
      projectID: "project-1",
      workspace: { workspaceID: "workspace-1" },
    }),
  ).rejects.toMatchObject({ detail: { kind: "prompt_catalog_read", command: null } });
});

it("accepts a server-selected child for prompt Send and Queue but rejects text and compaction retargeting", async () => {
  const child = "223e4567-e89b-42d3-a456-426614174000";
  const target = {
    kind: "session",
    projectID: "project-1",
    sessionID: "123e4567-e89b-42d3-a456-426614174000",
  } as const;
  const transport = new FakeRpcTransport([
    ...[ChatService.method.steer, ChatService.method.queue].map((descriptor) => ({
      descriptor,
      result: create(descriptor.output, {
        outcome: {
          case: "success",
          value: {
            session: { sessionId: child },
            outcome: {
              case: "accepted",
              value: {
                queueItem: { id: "323e4567-e89b-42d3-a456-426614174000" },
                disposition: InputAdmissionDisposition.STARTED_TURN,
              },
            },
          },
        },
      }),
    })),
    {
      descriptor: ChatService.method.compact,
      result: create(ChatService.method.compact.output, {
        outcome: {
          case: "success",
          value: {
            session: { sessionId: child },
            outcome: { case: "accepted", value: {} },
          },
        },
      }),
    },
  ]);
  const api = new ApiClient(transport, unexpectedProjectOverflow).chat;
  const command = {
    kind: "command",
    catalogIdentity: "prompt:review",
    token: "/review",
    separatorWhitespace: " ",
    arguments: "src",
  } as const;
  const queueItemID = parsePendingWorkItemID("323e4567-e89b-42d3-a456-426614174000");
  for (const method of [api.steer, api.queue]) {
    const accepted = await method(target, command, queueItemID);
    expect(accepted).toMatchObject({
      sessionID: child,
      outcome: { kind: "accepted" },
    });
    const invalidCommand = await method(target, { kind: "text", text: "ordinary text" }, queueItemID).then(
      () => {
        throw new Error("Expected plain text to be rejected as a command.");
      },
      (error: unknown) => error,
    );
    expect(invalidCommand).toBeInstanceOf(ContractError);
  }
  const invalidCompact = await api
    .compact(target, { token: "/compact", separatorWhitespace: "", rawGuidance: "" })
    .then(
      () => {
        throw new Error("Expected an empty compact command to be rejected.");
      },
      (error: unknown) => error,
    );
  expect(invalidCompact).toBeInstanceOf(ContractError);
});
