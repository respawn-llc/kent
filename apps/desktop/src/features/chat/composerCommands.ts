import type {
  ChatApi,
  ChatActivation,
  ChatCompactionResult,
  ChatInputMutationResult,
  ChatMutationTarget,
} from "@/api";

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
export type ComposerCommand = Readonly<{
  token: string;
  aliases: readonly string[];
  description: string | null;
  preview: string | null;
  execution:
    | Readonly<{ kind: "prompt"; catalogIdentity: string }>
    | Readonly<{ kind: "unavailable"; notify(): void }>
    | Readonly<{ kind: "direct"; send: DirectAction; queue?: DirectAction }>;
}>;
export type ComposerCommandResolution =
  | Readonly<{ kind: "input"; activation: ChatActivation }>
  | Readonly<{ kind: "unavailable"; notify(): void }>
  | Readonly<{
      kind: "direct";
      invocation: ComposerCommandInvocation;
      execution: Extract<ComposerCommand["execution"], { kind: "direct" }>;
    }>
  | Readonly<{ kind: "unknown-prompt"; token: string }>;

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
    return { kind: "unavailable", notify: command.execution.notify };
  if (invocation.token.startsWith("/prompt:")) return { kind: "unknown-prompt", token: invocation.token };
  return { kind: "input", activation: { kind: "text", text } };
}

export function composerSuggestions(
  text: string,
  commands: readonly ComposerCommand[],
): readonly ComposerCommand[] {
  const invocation = tokenizeComposerCommand(text);
  if (!invocation.token.startsWith("/") || invocation.separatorWhitespace.length > 0) return [];
  if (commands.some((command) => command.aliases.includes(invocation.token))) return [];
  return commands.filter((command) => command.token.startsWith(invocation.token));
}

export async function dispatchComposerCommand(
  api: ChatApi,
  target: ChatMutationTarget,
  command: Exclude<ComposerCommandResolution, { kind: "unknown-prompt" | "unavailable" }>,
  intent: "send" | "queue",
): Promise<ComposerCommandResult> {
  if (command.kind === "input") return api[intent === "send" ? "steer" : "queue"](target, command.activation);
  const action =
    intent === "queue" ? (command.execution.queue ?? command.execution.send) : command.execution.send;
  return action(target, command.invocation);
}
