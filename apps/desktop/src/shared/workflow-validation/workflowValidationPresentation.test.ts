import { expect, it } from "bun:test";
import { createInstance } from "i18next";
import { ValidationErrorCode, type WorkflowValidationError } from "@/api";
import { workflowValidationErrorMessage } from "./workflowValidationErrorMessage";
import { normalizeWorkflowValidationErrors } from "./workflowValidationIssueNormalization";

function issue(code: ValidationErrorCode): WorkflowValidationError {
  return {
    code,
    message: "server diagnostic",
    workflowID: null,
    nodeID: null,
    transitionGroupID: null,
    edgeID: null,
    details: {
      fieldName: "",
      inputName: "",
      placeholder: "",
      providerEdgeID: null,
      role: "",
      requiredTool: "",
    },
    relatedIDs: [],
    blocksContext: true,
  };
}

it("localizes the four Session reference issues and leaves ordinary server diagnostics unchanged", async () => {
  const i18n = createInstance();
  await i18n.init({ lng: "en", resources: {} });
  for (const [code, key] of [
    [ValidationErrorCode.SESSION_SOURCE_CANNOT_OWN_SESSION, "validationSessionSourceCannotOwnSession"],
    [ValidationErrorCode.SESSION_TRANSITION_MISSING, "validationSessionTransitionMissing"],
    [ValidationErrorCode.SESSION_TRANSITION_NOT_GUARANTEED, "validationSessionTransitionNotGuaranteed"],
    [ValidationErrorCode.SESSION_TRANSITION_AMBIGUOUS, "validationSessionTransitionAmbiguous"],
  ] as const) {
    expect(workflowValidationErrorMessage(issue(code), i18n.t)).toBe(i18n.t(`workflowEditor.${key}`));
  }
  const ordinary = issue(ValidationErrorCode.INVALID_NODE_KIND);
  expect(workflowValidationErrorMessage(ordinary, i18n.t)).toBe(ordinary.message);
});

it("deduplicates identical issues without merging distinct codes or changing first-seen ordering", () => {
  const first = issue(ValidationErrorCode.SESSION_TRANSITION_MISSING);
  const second = issue(ValidationErrorCode.SESSION_TRANSITION_AMBIGUOUS);
  const repeated = { ...first, relatedIDs: [...first.relatedIDs] };
  const result = normalizeWorkflowValidationErrors([first, second, repeated]);
  expect(result).toEqual([repeated, second]);
  expect(result[0]).toBe(repeated);
  expect(result[1]).toBe(second);
});
