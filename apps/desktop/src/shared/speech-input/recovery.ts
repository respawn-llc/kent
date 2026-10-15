import type { SpeechResult } from "@/api";
import type { SpeechSource } from "@/app-facade";
import type { SpeechTarget } from "./insertion";

export type Delivery =
  | Readonly<{ kind: "inserted" }>
  | Readonly<{ kind: "noop" }>
  | Readonly<{ kind: "copied" }>
  | Readonly<{ kind: "copy-failed"; text: string; error: unknown }>;

export async function deliverSpeech(
  source: SpeechSource,
  target: SpeechTarget,
  result: SpeechResult,
  actions: Readonly<{ discard(source: SpeechSource): Promise<void>; copy(text: string): Promise<void> }>,
): Promise<Delivery> {
  if (result.kind === "noop") {
    await actions.discard(source);
    return { kind: "noop" };
  }
  if (target.append(result.text)) {
    await actions.discard(source);
    return { kind: "inserted" };
  }
  try {
    await actions.copy(result.text);
    return { kind: "copied" };
  } catch (error) {
    return { kind: "copy-failed", text: result.text, error };
  }
}
