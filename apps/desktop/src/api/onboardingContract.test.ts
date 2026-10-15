import { create, validate } from "@app/server-api-contract";
import { FinalizeRequestSchema } from "@app/server-api-contract/gen/kent/api/onboarding/onboarding_pb";

describe("Onboarding disabled skills", () => {
  it("requires substantive names while preserving native normalization", () => {
    for (const name of [" \t", "\u0085", " Skill Name ", "\uFEFF"]) {
      const request = create(FinalizeRequestSchema, { disabledSkillNames: [name] });
      if (name === " Skill Name " || name === "\uFEFF") {
        expect(() => validate(FinalizeRequestSchema, request)).not.toThrow();
      } else {
        expect(() => validate(FinalizeRequestSchema, request)).toThrow();
      }
    }
  });
});
