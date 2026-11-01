import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import {
  FinalizeRequestSchema,
  FinalizeSuccessSchema,
} from "@app/server-api-contract/gen/kent/api/onboarding/onboarding_pb";

describe("Onboarding finalization", () => {
  it("requires successful completion", () => {
    for (const completed of [false, true]) {
      const response = create(FinalizeSuccessSchema, {
        completed,
        settingsPath: "/tmp/config.toml",
      });
      if (completed) {
        expect(() => validate(FinalizeSuccessSchema, response)).not.toThrow();
      } else {
        expect(() => validate(FinalizeSuccessSchema, response)).toThrow();
      }
    }
  });
});

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
