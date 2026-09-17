import { z } from "zod";

import { nonBlank } from "./chatSchemas";
import type { ChatProjectTarget, ChatSessionTarget } from "./chatTypes";

export function requireChatProjectTarget(target: ChatProjectTarget): void {
  if (!nonBlank.safeParse(target.projectID).success) throw new TypeError("Project ID is required.");
  const selector =
    "workspaceID" in target.workspace ? target.workspace.workspaceID : target.workspace.workspaceRoot;
  if (!nonBlank.safeParse(selector).success) throw new TypeError("Workspace selector is required.");
}

const exactSessionID = z
  .string()
  .min(1)
  .refine((value) => value.trim() === value);

export function isValidChatSessionID(value: string): boolean {
  return exactSessionID.safeParse(value).success;
}

export function requireChatSessionID(target: ChatSessionTarget): string {
  if (!nonBlank.safeParse(target.projectID).success) throw new TypeError("Project ID is required.");
  if (!isValidChatSessionID(target.sessionID)) throw new TypeError("Session ID is required.");
  return target.sessionID;
}
