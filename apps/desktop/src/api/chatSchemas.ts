import { z } from "zod";

export const nonBlank = z.string().refine((value) => value.trim().length > 0);
