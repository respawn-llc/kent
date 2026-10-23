import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import { SessionAttachmentSchema } from "@app/server-api-contract/gen/kent/api/connection/connection_pb";
import { ConnectionReplacementSchema } from "@app/server-api-contract/gen/kent/api/chat_settings/chat_settings_pb";
import {
  ConnectionDefinitionSchema,
  AuthMethod,
  BootstrapCompletionSchema,
  BootstrapStatusSchema,
  DeviceBootstrapContinuationSchema,
  ConnectionFactsSchema,
  ConnectionFailureDetailsSchema,
  ConnectionFailureReason,
  ConnectionProtocol,
  ConnectionReferenceEditSchema,
  ConnectionSelectionSchema,
  ConnectionTargetSchema,
  ConfigureConnectionRequestSchema,
  ProviderSelectionSchema,
  ProviderKind,
  StatusFactsSchema,
} from "@app/server-api-contract/gen/kent/api/auth/auth_pb";

it("rejects sign-in polling intervals that overflow a duration", () => {
  for (const pollIntervalSeconds of [9223372036n, 9223372037n]) {
    const continuation = create(DeviceBootstrapContinuationSchema, {
      code: "device-code",
      userCode: "user-code",
      pollIntervalSeconds,
    });
    const check = () => validate(DeviceBootstrapContinuationSchema, continuation);
    if (pollIntervalSeconds === 9223372036n) {
      expect(check).not.toThrow();
    } else {
      expect(check).toThrow();
    }
  }
});

describe("Session attachment capabilities", () => {
  it("accepts nonempty capabilities without surrounding whitespace", () => {
    for (const capability of [
      "signed-capability",
      "",
      " signed-capability",
      "signed-capability\t",
      "\u0085signed-capability",
      " \t",
    ]) {
      const attachment = create(SessionAttachmentSchema, {
        projectId: "project-1",
        workspaceId: "workspace-1",
        workspaceRoot: "/workspace",
        sessionId: "00000000-0000-4000-8000-000000000001",
        reattachCapability: capability,
      });
      const check = () => validate(SessionAttachmentSchema, attachment);
      if (capability === "signed-capability") {
        expect(check).not.toThrow();
      } else {
        expect(check).toThrow();
      }
    }
  });
});

describe("Connection Selection identities", () => {
  it("requires Connection IDs in Chat Settings replacements", () => {
    for (const id of ["openai", "local-2_api", "", "Bad", " padded ", "bad/name", "\uFEFF"]) {
      for (const field of ["previous", "current"] as const) {
        const replacement = create(ConnectionReplacementSchema, {
          previous: "openai",
          current: "local",
          [field]: id,
        });
        const check = () => validate(ConnectionReplacementSchema, replacement);
        if (id === "openai" || id === "local-2_api") {
          expect(check).not.toThrow();
        } else {
          expect(check).toThrow();
        }
      }
    }
  });

  it("requires the same alphabet in Connection outputs", () => {
    for (const id of ["openai", "local-2_api", "Bad", " padded ", "bad/name", "é"]) {
      const checks = [
        () => validate(BootstrapStatusSchema, create(BootstrapStatusSchema, { connectionId: id })),
        () => validate(BootstrapCompletionSchema, create(BootstrapCompletionSchema, { connectionId: id })),
        () =>
          validate(
            ConnectionFactsSchema,
            create(ConnectionFactsSchema, {
              connectionId: id,
              method: AuthMethod.NONE,
            }),
          ),
        () =>
          validate(
            ConnectionFailureDetailsSchema,
            create(ConnectionFailureDetailsSchema, {
              connectionId: id,
              reason: ConnectionFailureReason.NOT_FOUND,
            }),
          ),
        () =>
          validate(
            StatusFactsSchema,
            create(StatusFactsSchema, {
              connectionId: id,
              method: AuthMethod.NONE,
              provider: { kind: ProviderKind.OPENAI, identifier: "openai" },
              methodFacts: { case: "noAuth", value: {} },
            }),
          ),
      ];
      for (const check of checks) {
        if (id === "openai" || id === "local-2_api") {
          expect(check).not.toThrow();
        } else {
          expect(check).toThrow();
        }
      }
    }
  });

  it("accepts the configured Connection ID alphabet", () => {
    for (const id of ["openai", "local-2_api", "Bad", " padded ", "bad/name", "é"]) {
      const selection = create(ConnectionSelectionSchema, { ids: [id] });
      if (id === "openai" || id === "local-2_api") {
        expect(() => validate(ConnectionSelectionSchema, selection)).not.toThrow();
      } else {
        expect(() => validate(ConnectionSelectionSchema, selection)).toThrow();
      }
    }
  });

  it("requires the same alphabet for Connection request identities", () => {
    for (const id of ["openai", "local-2_api", "Bad", " padded ", "bad/name", "é"]) {
      const checks = [
        () =>
          validate(
            ConnectionDefinitionSchema,
            create(ConnectionDefinitionSchema, {
              id,
              protocol: ConnectionProtocol.CHATGPT,
            }),
          ),
        () =>
          validate(
            ConnectionTargetSchema,
            create(ConnectionTargetSchema, {
              target: { case: "connectionId", value: id },
            }),
          ),
        () => validate(ProviderSelectionSchema, create(ProviderSelectionSchema, { connectionId: id })),
        () =>
          validate(
            ConnectionReferenceEditSchema,
            create(ConnectionReferenceEditSchema, {
              connectionId: id,
              environmentVariable: "API_KEY",
            }),
          ),
        () =>
          validate(
            ConfigureConnectionRequestSchema,
            create(ConfigureConnectionRequestSchema, {
              change: { case: "defaultConnectionId", value: id },
            }),
          ),
      ];
      for (const check of checks) {
        if (id === "openai" || id === "local-2_api") {
          expect(check).not.toThrow();
        } else {
          expect(check).toThrow();
        }
      }
    }
  });
});
