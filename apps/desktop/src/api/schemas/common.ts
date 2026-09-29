import { z } from "zod";

export const nonBlankString = z.string().trim().min(1);
