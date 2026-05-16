import { z } from "zod";

import type { ServerCapabilities, ServerReadiness } from "../models";

export const readinessSchema: z.ZodType<ServerReadiness> = z
  .object({
    ready: z.boolean(),
    server_id: z.string(),
    server_version: z.string(),
    protocol_version: z.string(),
    auth_ready: z.boolean(),
    auth_required: z.boolean(),
    endpoint: z.string(),
    causes: z
      .array(
        z.object({
          code: z.string(),
          severity: z.string(),
          summary: z.string(),
          next_action: z.string(),
          diagnostic_id: z.string().optional().default(""),
        }),
      )
      .optional()
      .default([]),
  })
  .transform((value) => ({
    ready: value.ready,
    serverID: value.server_id,
    serverVersion: value.server_version,
    protocolVersion: value.protocol_version,
    authReady: value.auth_ready,
    authRequired: value.auth_required,
    endpoint: value.endpoint,
    causes: value.causes.map((cause) => ({
      code: cause.code,
      severity: cause.severity,
      summary: cause.summary,
      nextAction: cause.next_action,
      diagnosticID: cause.diagnostic_id,
    })),
  }));

export const capabilitiesSchema: z.ZodType<ServerCapabilities> = z
  .object({
    capabilities: z.array(
      z.object({
        id: z.string(),
        available: z.boolean(),
        reason: z.string().optional().default(""),
        required_for_mvp: z.boolean(),
      }),
    ),
    server_version: z.string(),
    protocol_version: z.string(),
  })
  .transform((value) => ({
    capabilities: value.capabilities.map((capability) => ({
      id: capability.id,
      available: capability.available,
      reason: capability.reason,
      requiredForMvp: capability.required_for_mvp,
    })),
    serverVersion: value.server_version,
    protocolVersion: value.protocol_version,
  }));
