import { create } from "@app/server-api-contract";
import { NewChatTargetSchema } from "@app/server-api-contract/gen/kent/api/chat/chat_pb";
import type { ChatMutationTarget } from "./chatTypes";
import { initialChatSettingsToWire } from "./chatSettings";

export function newChatToWire(
  target: Extract<ChatMutationTarget, { kind: "new_chat" }>,
  workspaceID: string,
) {
  return create(NewChatTargetSchema, {
    projectId: target.projectID,
    workspaceId: workspaceID,
    initialSettings: initialChatSettingsToWire(target.initialSettings),
    ...(target.name === null ? {} : { name: target.name }),
    ...(target.worktreeSelector === null ? {} : { worktreeSelector: target.worktreeSelector }),
  });
}
