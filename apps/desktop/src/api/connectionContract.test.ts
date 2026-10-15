import { create, validate } from "@app/server-api-contract";
import {
  ConnectionDefinitionSchema,
  ConnectionProtocol,
  ConnectionReferenceEditSchema,
  ConnectionSelectionSchema,
  ConnectionTargetSchema,
  ConfigureConnectionRequestSchema,
  ProviderSelectionSchema,
} from "@app/server-api-contract/gen/kent/api/auth/auth_pb";

describe("Connection Selection identities", () => {
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
