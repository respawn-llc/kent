import type {
  ChatApi,
  ChatActivation,
  ChatCompactionResult,
  ChatInputMutationResult,
  ChatMutationTarget,
  ChatMainView,
  PendingWorkItemID,
} from "@/api";
import type { QueryClient } from "@tanstack/react-query";
import { isCompacting, queryKeys } from "@/app-facade";

export type ComposerCommandInvocation = Readonly<{
  token: string;
  separatorWhitespace: string;
  arguments: string;
}>;
export type ComposerCommandResult =
  ChatInputMutationResult | ChatCompactionResult | Readonly<{ kind: "local" }>;
type DirectAction = (
  target: ChatMutationTarget,
  invocation: ComposerCommandInvocation,
) => Promise<ComposerCommandResult>;

export function compactCommand(api: ChatApi, client: QueryClient, description: string): ComposerCommand {
  return {
    token: "/compact",
    aliases: [],
    description,
    preview: null,
    execution: {
      kind: "direct",
      send: async (target, invocation) => {
        if (
          target.kind === "session" &&
          isCompacting(client.getQueryData<ChatMainView>(queryKeys.chatMainView(target.sessionID))?.activity)
        )
          return {
            sessionID: target.sessionID,
            outcome: { kind: "not_accepted", reason: { kind: "active" } },
          };
        return api.compact(target, {
          token: "/compact",
          separatorWhitespace: invocation.separatorWhitespace,
          rawGuidance: invocation.arguments,
        });
      },
    },
  };
}
export type ComposerCommand = Readonly<{
  token: string;
  aliases: readonly string[];
  description: string | null;
  preview: string | null;
  execution:
    | Readonly<{ kind: "prompt"; catalogIdentity: string }>
    | Readonly<{ kind: "unavailable"; draft: "restore" | "discard"; notify(): void }>
    | Readonly<{ kind: "direct"; send: DirectAction; queue?: DirectAction }>;
}>;
export type ComposerCommandResolution =
  | Readonly<{ kind: "input"; activation: ChatActivation }>
  | Readonly<{ kind: "unavailable"; draft: "restore" | "discard"; notify(): void }>
  | Readonly<{
      kind: "direct";
      invocation: ComposerCommandInvocation;
      execution: Extract<ComposerCommand["execution"], { kind: "direct" }>;
    }>
  | Readonly<{ kind: "unknown-prompt"; token: string }>;
export type PreparedComposerCommand =
  | (Extract<ComposerCommandResolution, { kind: "input" }> & Readonly<{ queueItemID: PendingWorkItemID }>)
  | Extract<ComposerCommandResolution, { kind: "direct" }>;

export function tokenizeComposerCommand(text: string): ComposerCommandInvocation {
  let tokenStart = 0;
  while (tokenStart < text.length && text.charAt(tokenStart).trim().length === 0) tokenStart++;
  let tokenEnd = tokenStart;
  while (tokenEnd < text.length && text.charAt(tokenEnd).trim().length > 0) tokenEnd++;
  let argumentsStart = tokenEnd;
  while (argumentsStart < text.length && text.charAt(argumentsStart).trim().length === 0) argumentsStart++;
  return {
    token: text.slice(tokenStart, tokenEnd),
    separatorWhitespace: text.slice(tokenEnd, argumentsStart),
    arguments: text.slice(argumentsStart),
  };
}

export function resolveComposerCommand(
  text: string,
  commands: readonly ComposerCommand[],
): ComposerCommandResolution {
  const invocation = tokenizeComposerCommand(text);
  const command = commands.find(
    (entry) => entry.token === invocation.token || entry.aliases.includes(invocation.token),
  );
  if (command?.execution.kind === "prompt")
    return {
      kind: "input",
      activation: { kind: "command", catalogIdentity: command.execution.catalogIdentity, ...invocation },
    };
  if (command?.execution.kind === "direct")
    return { kind: "direct", invocation, execution: command.execution };
  if (command?.execution.kind === "unavailable")
    return { kind: "unavailable", draft: command.execution.draft, notify: command.execution.notify };
  if (invocation.token.startsWith("/prompt:")) return { kind: "unknown-prompt", token: invocation.token };
  return { kind: "input", activation: { kind: "text", text } };
}

export function composerSuggestions(
  text: string,
  commands: readonly ComposerCommand[],
): readonly ComposerCommand[] {
  const invocation = tokenizeComposerCommand(text);
  if (!invocation.token.startsWith("/") || invocation.separatorWhitespace.length > 0) return [];
  const query = invocation.token.slice(1).toLowerCase();
  // Match the TUI registry: earliest occurrence, shortest name, then lexical order.
  return commands
    .filter((command) => command.execution.kind !== "unavailable")
    .map((command) => ({ command, index: command.token.slice(1).indexOf(query) }))
    .filter(({ index }) => index >= 0)
    .sort(
      (a, b) =>
        a.index - b.index ||
        a.command.token.length - b.command.token.length ||
        (a.command.token < b.command.token ? -1 : a.command.token > b.command.token ? 1 : 0),
    )
    .map(({ command }) => command);
}

export async function dispatchComposerCommand(
  api: ChatApi,
  target: ChatMutationTarget,
  command: PreparedComposerCommand,
  intent: "send" | "queue",
): Promise<ComposerCommandResult> {
  if (command.kind === "input")
    return api[intent === "send" ? "steer" : "queue"](target, command.activation, command.queueItemID);
  const action =
    intent === "queue" ? (command.execution.queue ?? command.execution.send) : command.execution.send;
  return action(target, command.invocation);
}
