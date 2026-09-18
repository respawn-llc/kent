import type { ChatApi } from "@/api";
import type { TFunction } from "i18next";
import type { ComposerCommand } from "./composerCommands";

const builtins = [
  { identity: "prompt:review", alias: "/review", description: "chatComposer.commands.review" },
  { identity: "prompt:init", alias: "/init", description: "chatComposer.commands.init" },
] as const;

export function promptCommands(
  catalog: Awaited<ReturnType<ChatApi["getCommandCatalog"]>>,
  t: TFunction,
): readonly ComposerCommand[] {
  return [
    ...builtins.map((builtin): ComposerCommand => ({
      token: `/${builtin.identity}`,
      aliases: [builtin.alias],
      description: t(builtin.description),
      preview: null,
      execution: { kind: "prompt", catalogIdentity: builtin.identity },
    })),
    ...catalog
      .filter((entry) => !builtins.some((builtin) => builtin.identity === entry.name))
      .map((entry): ComposerCommand => ({
        token: `/${entry.name}`,
        aliases: [],
        description: null,
        preview: entry.preview,
        execution: { kind: "prompt", catalogIdentity: entry.name },
      })),
  ];
}
