import type { ChatApi } from "@/api";
import type { ComposerCommand } from "./composerCommands";

export function createNameCommand({
  api,
  existingSession,
  description,
  notify,
}: Readonly<{
  api: ChatApi;
  existingSession: boolean;
  description: string | null;
  notify(): void;
}>): ComposerCommand {
  return {
    token: "/name",
    aliases: [],
    description,
    preview: null,
    execution: existingSession
      ? {
          kind: "direct",
          send: async (target, invocation) => {
            if (target.kind !== "session") throw new Error("Session name requires an existing Session.");
            const name = invocation.arguments.trim();
            await api.setSessionName(target, name.length === 0 ? { kind: "clear" } : { kind: "set", name });
            return { kind: "local" };
          },
        }
      : { kind: "unavailable", draft: "restore", notify },
  };
}
