import { describe, expect, it } from "bun:test";
import { create, validate } from "@app/server-api-contract";
import {
  ToolRowSchema,
  ToolStartSchema,
} from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import {
  ToolRenderHintSchema,
  ToolRenderKind,
  ToolPresentationKind,
} from "@app/server-api-contract/gen/kent/api/transcript/tool_presentation_pb";

describe("Tool owner contract", () => {
  it.each([" patch ", " edit ", " replace ", " write "])(
    "rejects normalized family %s without patch",
    (name) => {
      expect(() => validate(ToolRowSchema, create(ToolRowSchema, { toolName: name }))).toThrow();
      expect(() =>
        validate(
          ToolStartSchema,
          create(ToolStartSchema, {
            stepId: "22222222-2222-4222-8222-222222222222",
            toolCallId: "call-1",
            toolName: name,
          }),
        ),
      ).toThrow();
    },
  );

  it("accepts nonfamily owners without presentation", () => {
    expect(() => validate(ToolRowSchema, create(ToolRowSchema, { toolName: "shell" }))).not.toThrow();
    expect(() =>
      validate(
        ToolStartSchema,
        create(ToolStartSchema, {
          stepId: "22222222-2222-4222-8222-222222222222",
          toolCallId: "call-1",
          toolName: "shell",
        }),
      ),
    ).not.toThrow();
  });

  it("requires substantive Source paths without requiring paths for other kinds", () => {
    for (const path of [undefined, " \t "]) {
      expect(() =>
        validate(ToolRenderHintSchema, create(ToolRenderHintSchema, { kind: ToolRenderKind.SOURCE, path })),
      ).toThrow();
    }
    expect(() =>
      validate(
        ToolRenderHintSchema,
        create(ToolRenderHintSchema, { kind: ToolRenderKind.SOURCE, path: "file.go" }),
      ),
    ).not.toThrow();
    for (const kind of [ToolRenderKind.SHELL, ToolRenderKind.DIFF, ToolRenderKind.PLAIN]) {
      expect(() => validate(ToolRenderHintSchema, create(ToolRenderHintSchema, { kind }))).not.toThrow();
    }
  });

  it("requires the enclosing tool name for a presentation", () => {
    const presentation = {
      presentation: ToolPresentationKind.DEFAULT,
      renderBehavior: ToolPresentationKind.DEFAULT,
    };
    expect(() => validate(ToolRowSchema, create(ToolRowSchema, { presentation }))).toThrow();
    expect(() =>
      validate(ToolRowSchema, create(ToolRowSchema, { toolName: "shell", presentation })),
    ).not.toThrow();
  });
});
