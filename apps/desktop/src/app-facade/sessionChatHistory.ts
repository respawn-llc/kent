import { z } from "zod";

const sessionChatCatalogOriginSchema = z.object({
  category: z.enum(["main", "subagent"]),
});

export const sessionChatHistoryStateSchema = z.looseObject({
  sessionChat: z
    .object({
      catalogOrigin: sessionChatCatalogOriginSchema.nullable(),
      projectID: z.string().min(1),
      openingID: z.uuid().optional(),
      deliveredSessionID: z.string().min(1).nullable().optional(),
    })
    .nullable()
    .optional(),
});

export type SessionChatCatalogOrigin = z.output<typeof sessionChatCatalogOriginSchema>;
export type SessionChatHistoryState = z.output<typeof sessionChatHistoryStateSchema>;

export function displayedChatSessionID(
  historyState: unknown,
  projectID: string | undefined,
  routeSessionID: string | null | undefined,
): string | null {
  const metadata = sessionChatHistoryStateSchema.parse(historyState).sessionChat;
  return metadata?.projectID === projectID
    ? (metadata?.deliveredSessionID ?? routeSessionID ?? null)
    : (routeSessionID ?? null);
}
