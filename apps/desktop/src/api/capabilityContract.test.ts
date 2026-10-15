import { create, validate } from "@app/server-api-contract";
import { ModelFactSchema, ModelFactsSchema } from "@app/server-api-contract/gen/kent/api/capability/capability_pb";

describe("Capability model facts", () => {
  it("requires substantive thinking levels without changing whitespace normalization", () => {
    for (const level of [" \t", "\u0085", " high ", "\uFEFF"]) {
      const fact = create(ModelFactSchema, {
        supportedThinkingLevels: [level], verbosity: { source: "catalog" },
      });
      if (level === " high " || level === "\uFEFF") {
        expect(() => validate(ModelFactSchema, fact)).not.toThrow();
      } else {
        expect(() => validate(ModelFactSchema, fact)).toThrow();
      }
    }
  });

  it("requires substantive known-model identities while permitting an unnamed fallback", () => {
    for (const [modelId, known, valid] of [
      [undefined, true, false],
      [" \t", true, false],
      ["model", false, false],
      [" model ", true, true],
      ["\uFEFF", true, true],
    ] as const) {
      const facts = create(ModelFactsSchema, {
        knownModels: [{ modelId, known, verbosity: { source: "catalog" } }],
        unknownFallback: { verbosity: { source: "fallback" } },
      });
      if (valid) {
        expect(() => validate(ModelFactsSchema, facts)).not.toThrow();
      } else {
        expect(() => validate(ModelFactsSchema, facts)).toThrow();
      }
    }
  });
});
