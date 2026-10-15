import { create, validate } from "@app/server-api-contract";
import {
  DefaultFactsSchema,
  ImportChoiceMode,
  ImportChoiceRefSchema,
  ImportItemKind,
  ImportItemRefSchema,
  ImportSourceKind,
  SkillEnablementProjectionFactSchema,
  ThinkingDefaultFactSchema,
  ModelFactSchema,
  ModelFactsSchema,
  ModelVerbosityFactSchema,
} from "@app/server-api-contract/gen/kent/api/capability/capability_pb";
import { SupervisorFrequency } from "@app/server-api-contract/gen/kent/api/onboarding/onboarding_pb";

describe("Capability model facts", () => {
  it("accepts exactly the supported default thinking modes without requiring optional payloads", () => {
    for (const mode of ["default", "disabled", "level", "custom", "unsupported", " default "]) {
      const fact = create(ThinkingDefaultFactSchema, { mode });
      if (mode === "unsupported" || mode === " default ") {
        expect(() => validate(ThinkingDefaultFactSchema, fact)).toThrow();
      } else {
        expect(() => validate(ThinkingDefaultFactSchema, fact)).not.toThrow();
      }
    }
  });

  it("requires a substantive default model while preserving normalization", () => {
    for (const primaryModelId of [" \t", "\u0085", " model ", "\uFEFF"]) {
      const defaults = create(DefaultFactsSchema, {
        primaryModelId, thinking: { mode: "default" }, compactionMode: "native",
        supervisor: { frequency: SupervisorFrequency.OFF },
      });
      if (primaryModelId === " model " || primaryModelId === "\uFEFF") {
        expect(() => validate(DefaultFactsSchema, defaults)).not.toThrow();
      } else {
        expect(() => validate(DefaultFactsSchema, defaults)).toThrow();
      }
    }
  });

  it("requires substantive candidate names only in Skill Enablement projections", () => {
    for (const targetName of [" \t", "\u0085", " skill ", "\uFEFF"]) {
      const ref = create(ImportItemRefSchema, {
        targetName,
        itemKind: ImportItemKind.SKILL,
        sourceKind: ImportSourceKind.GLOBAL,
      });
      expect(() => validate(ImportItemRefSchema, ref)).not.toThrow();
      const projection = create(SkillEnablementProjectionFactSchema, {
        choiceRef: { mode: ImportChoiceMode.NONE },
        candidates: [{ ref }],
      });
      if (targetName === " skill " || targetName === "\uFEFF") {
        expect(() => validate(SkillEnablementProjectionFactSchema, projection)).not.toThrow();
      } else {
        expect(() => validate(SkillEnablementProjectionFactSchema, projection)).toThrow();
      }
    }
  });

  it("requires a substantive provider and root only for symlink choices", () => {
    expect(() =>
      validate(
        ImportChoiceRefSchema,
        create(ImportChoiceRefSchema, {
          mode: ImportChoiceMode.NONE,
        }),
      ),
    ).not.toThrow();
    for (const [importProviderId, sourceRootPath, valid] of [
      [undefined, "/source", false],
      ["provider", undefined, false],
      [" \t", "/source", false],
      ["provider", " \t", false],
      [" provider ", " /source ", true],
      ["\uFEFF", "\uFEFF", true],
    ] as const) {
      const ref = create(ImportChoiceRefSchema, {
        mode: ImportChoiceMode.SYMLINK_SOURCE,
        importProviderId,
        sourceRootPath,
      });
      if (valid) {
        expect(() => validate(ImportChoiceRefSchema, ref)).not.toThrow();
      } else {
        expect(() => validate(ImportChoiceRefSchema, ref)).toThrow();
      }
    }
  });

  it("accepts only the existing trimmed verbosity levels", () => {
    for (const level of ["low", " medium ", "high", " \t", "custom"]) {
      const fact = create(ModelVerbosityFactSchema, { source: "catalog", levels: [level] });
      if (level === "low" || level === " medium " || level === "high") {
        expect(() => validate(ModelVerbosityFactSchema, fact)).not.toThrow();
      } else {
        expect(() => validate(ModelVerbosityFactSchema, fact)).toThrow();
      }
    }
  });

  it("requires substantive thinking levels without changing whitespace normalization", () => {
    for (const level of [" \t", "\u0085", " high ", "\uFEFF"]) {
      const fact = create(ModelFactSchema, {
        supportedThinkingLevels: [level],
        verbosity: { source: "catalog" },
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
