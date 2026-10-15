import { create, validate } from "@app/server-api-contract";
import { ConnectionSelectionSchema } from "@app/server-api-contract/gen/kent/api/auth/auth_pb";

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
});
