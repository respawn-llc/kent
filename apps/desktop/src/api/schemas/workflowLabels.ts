import { z } from "zod";

import type { ProjectLabel } from "../workflowLabels";
import { workflowLabelMaxIDs } from "../workflowLabelContract";

export const labelIDSchema = z.uuidv4();
export const labelIDListSchema = z
  .array(labelIDSchema)
  .max(workflowLabelMaxIDs)
  .superRefine((labelIDs, context) => {
    const seen = new Set<string>();
    for (const [index, labelID] of labelIDs.entries()) {
      if (seen.has(labelID)) {
        context.addIssue({
          code: "custom",
          message: "label IDs must be unique",
          path: [index],
        });
      }
      seen.add(labelID);
    }
  });

export const projectLabelSchema: z.ZodType<ProjectLabel> = z
  .object({
    id: labelIDSchema,
    name: z.string().min(1),
  })
  .strict();
