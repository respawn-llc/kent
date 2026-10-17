import { expect, it } from "bun:test";
import { create, decode, encode } from "@app/server-api-contract";
import {
  ManualCompactionAdmissionSchema,
  PendingWorkMessageSchema,
  PendingWorkWorktreeTransitionKind,
  PendingWorkWorktreeTransitionSchema,
} from "@app/server-api-contract/gen/kent/api/runtime/runtime_pb";

it.each([
  ["ordinary 日本語 text", true],
  ["\uFEFFsubstantive\uFEFF", true],
  ["\uFEFF \t\u0085", false],
  ["\u0085", false],
])("enforces scoped Pending Work nonblank content: %j", (text, valid) => {
  const encodeMessage = () => encode(PendingWorkMessageSchema, create(PendingWorkMessageSchema, { text }));
  if (valid) {
    expect(() => decode(PendingWorkMessageSchema, encodeMessage())).not.toThrow();
  } else {
    expect(encodeMessage).toThrow();
  }
});

it.each([
  ["keep 日本語 details", true],
  ["keep  details", false],
  ["keep\uFEFFdetails", false],
  ["keep\u0085details", false],
])("enforces canonical Pending Work arguments: %j", (value, valid) => {
  const encodeGuidance = () =>
    encode(ManualCompactionAdmissionSchema, create(ManualCompactionAdmissionSchema, { guidance: value }));
  const encodeSelector = () =>
    encode(
      PendingWorkWorktreeTransitionSchema,
      create(PendingWorkWorktreeTransitionSchema, {
        transition: PendingWorkWorktreeTransitionKind.ENTER,
        selector: value,
      }),
    );
  for (const encodeArgument of [encodeGuidance, encodeSelector]) {
    if (valid) expect(encodeArgument).not.toThrow();
    else expect(encodeArgument).toThrow();
  }
});
