import type { ChatApi } from "@/api";
import type { TFunction } from "i18next";
import { showStatusToast } from "@/ui";
import type { ComposerCommand } from "./composerCommands";

export function chatDestinationCommands(
  api: ChatApi,
  newChat: boolean,
  t: TFunction,
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
  return newChat
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
}
