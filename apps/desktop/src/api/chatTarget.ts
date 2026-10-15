import { z } from "zod";

const exactSessionID = z
  .string()
  .min(1)
  .refine((value) => value.trim() === value);

export function isValidChatSessionID(value: string): boolean {
  return exactSessionID.safeParse(value).success;
}
