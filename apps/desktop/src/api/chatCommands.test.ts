import { create } from "@app/server-api-contract";
import { PromptCommandService } from "@app/server-api-contract/gen/kent/api/prompt_command/prompt_command_pb";
import { FakeRpcTransport, unexpectedProjectOverflow } from "@/test-support/api";
import { ApiClient } from "./client";
import { ChatService } from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import { ContractError } from "./errors";

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
  await expect(
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
              value: { queueItem: { id: "323e4567-e89b-42d3-a456-426614174000" } },
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
            outcome: { case: "accepted", value: { request: { id: "323e4567-e89b-42d3-a456-426614174000" } } },
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
  for (const method of [api.steer, api.queue]) {
    await expect(method(target, command)).resolves.toMatchObject({
      sessionID: child,
      outcome: { kind: "accepted" },
    });
    await expect(method(target, { kind: "text", text: "ordinary text" })).rejects.toBeInstanceOf(
      ContractError,
    );
  }
  await expect(
    api.compact(target, { token: "/compact", separatorWhitespace: "", rawGuidance: "" }),
  ).rejects.toBeInstanceOf(ContractError);
});
