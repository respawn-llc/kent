export type SubscriptionConnection = Readonly<{ id: string; supported: boolean }>;
export type SubscriptionBinding = Readonly<{ connectionID: string; supported: boolean }>;
export type SubscriptionWindow = Readonly<{
  bucket: "default" | "additional";
  durationSeconds: number;
  usedPercent: number;
  resetAt: number | null;
  limitName: string | null;
  meteredFeature: string | null;
  contributions: Readonly<{ build: number | null; chat: number | null }> | null;
}>;
export type SubscriptionUsage = Readonly<{
  plan: string | null;
  source: "chatgpt" | "grok" | null;
  windows: readonly SubscriptionWindow[];
}>;
export class SubscriptionReadError extends Error {
  readonly source: SubscriptionUsage["source"];
  readonly classification: "unavailable-for-sign-in-method" | null;
  readonly binding: SubscriptionBinding | null;
  constructor(
    message: string,
    readonly kind: "binding" | "auth" | "quota",
    facts: Readonly<{
      source?: SubscriptionUsage["source"];
      classification?: "unavailable-for-sign-in-method" | null;
      binding?: SubscriptionBinding | null;
    }> = {},
  ) {
    super(message);
    this.source = facts.source ?? null;
    this.classification = facts.classification ?? null;
    this.binding = facts.binding ?? null;
  }
}
