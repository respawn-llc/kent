import type { ChatApi } from "@/api";
import type { ComposerCommand } from "./composerCommands";

export function createNameCommand({
  api,
  description,
  setTemporaryName,
}: Readonly<{
  api: ChatApi;
  description: string | null;
  setTemporaryName(name: string | null): void;
}>): ComposerCommand {
  return {
    token: "/name",
    aliases: [],
    description,
    preview: null,
    execution: {
      kind: "direct",
      send: async (target, invocation) => {
        const name = invocation.arguments.trim();
        if (target.kind === "session")
          await api.setSessionName(target, name.length === 0 ? { kind: "clear" } : { kind: "set", name });
        else setTemporaryName(name.length === 0 ? null : name);
        return { kind: "local" };
      },
    },
  };
}
