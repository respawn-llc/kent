import type { SpeechResult } from "@/api";
import type { SpeechTarget } from "./insertion";

export type Delivery =
  | Readonly<{ kind: "inserted" }>
  | Readonly<{ kind: "noop" }>
  | Readonly<{ kind: "copied" }>
  | Readonly<{ kind: "copy-failed"; text: string; error: unknown }>;

export async function deliverSpeech(
  target: SpeechTarget,
  result: SpeechResult,
  actions: Readonly<{ copy(text: string): Promise<void> }>,
): Promise<Delivery> {
  if (result.kind === "noop" || target.append(result.text)) {
    return { kind: result.kind === "noop" ? "noop" : "inserted" };
  }
  try {
    await actions.copy(result.text);
    return { kind: "copied" };
  } catch (error) {
    return { kind: "copy-failed", text: result.text, error };
  }
}
