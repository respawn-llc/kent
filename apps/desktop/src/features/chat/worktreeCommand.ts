import type { TFunction } from "i18next";
import type { StatusController } from "@/app-facade";
import { tokenizeComposerCommand, type ComposerCommand } from "./composerCommands";

export type WorktreeCommandIntent =
  | Readonly<{ kind: "list" | "create" | "leave" }>
  | Readonly<{ kind: "switch"; selector: string }>
  | Readonly<{ kind: "delete"; selector: string | null }>;

export function createWorktreeCommand({
  execute,
  push,
  t,
}: Readonly<{
  execute: ((sessionID: string, intent: WorktreeCommandIntent) => void | Promise<void>) | null;
  push: StatusController["push"];
  t: TFunction;
}>): ComposerCommand {
  return {
    token: "/worktree",
    aliases: ["/wt"],
    description: t("chat.worktree.title"),
    preview: null,
    execution: {
      kind: "direct",
      send: async (target, invocation) => {
        if (target.kind === "session" && execute !== null) {
          const intent = parseArguments(invocation.arguments);
          if (intent !== null) await execute(target.sessionID, intent);
          else
            push({
              id: crypto.randomUUID(),
              tone: "danger",
              title: t("chat.worktree.title"),
              body: t("chat.worktree.commandUsage"),
            });
        } else
          push({
            id: crypto.randomUUID(),
            tone: "danger",
            title: t("chat.worktree.title"),
            body: t("chat.worktree.sessionRequired"),
          });
        return { kind: "local" };
      },
    },
  };
}

const subcommands = new Map<string, WorktreeCommandIntent["kind"]>([
  ["status", "list"],
  ["new", "create"],
  ["create", "create"],
  ["leave", "leave"],
  ["switch", "switch"],
  ["delete", "delete"],
  ["remove", "delete"],
  ["rm", "delete"],
]);

function argumentWords(text: string): readonly string[] {
  const words: string[] = [];
  let remaining = text;
  while (remaining.trim().length > 0) {
    const word = tokenizeComposerCommand(remaining);
    words.push(word.token);
    remaining = word.arguments;
  }
  return words;
}

function parseArguments(text: string): WorktreeCommandIntent | null {
  const [subcommand, ...targets] = argumentWords(text);
  const kind = subcommand === undefined ? "list" : subcommands.get(subcommand.toLowerCase());
  switch (kind) {
    case "list":
    case "create":
    case "leave":
      return targets.length === 0 ? { kind } : null;
    case "switch":
      return targets.length > 0 ? { kind: "switch", selector: targets.join(" ") } : null;
    case "delete":
      return targets.length <= 1 ? { kind: "delete", selector: targets[0] ?? null } : null;
    case undefined:
      return null;
  }
}
