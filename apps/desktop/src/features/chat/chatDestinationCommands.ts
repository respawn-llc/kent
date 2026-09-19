import type { ChatApi } from "@/api";
import type { TFunction } from "i18next";
import { showStatusToast } from "@/ui";
import type { ComposerCommand } from "./composerCommands";
import { promptCommands } from "./promptCommands";

export function chatDestinationCommands(
  api: ChatApi,
  newChat: boolean,
  t: TFunction,
  catalog: Awaited<ReturnType<ChatApi["getCommandCatalog"]>> = [],
): readonly ComposerCommand[] {
  const compact: ComposerCommand = {
    token: "/compact",
    aliases: [],
    description: null,
    preview: null,
    execution: {
      kind: "direct",
      send: async (target, invocation) =>
        api.compact(target, {
          token: "/compact",
          separatorWhitespace: invocation.separatorWhitespace,
          rawGuidance: invocation.arguments,
        }),
    },
  };
  const nativeCommands: readonly ComposerCommand[] = newChat
    ? [
        compact,
        {
          token: "/worktree",
          aliases: ["/wt"],
          description: null,
          preview: null,
          execution: {
            kind: "unavailable",
            notify: () => {
              showStatusToast({
                id: "chat-worktree-session-required",
                tone: "danger",
                title: t("chatComposer.submitFailed"),
                body: t("chat.worktreeSessionRequired"),
              });
            },
          },
        },
      ]
    : [compact];
  return [...nativeCommands, ...promptCommands(catalog, t)];
}
