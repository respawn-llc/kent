import { create, decode, encode } from "@app/server-api-contract";
import * as T from "@app/server-api-contract/gen/kent/api/transcript/tool_presentation_pb";
import * as R from "@app/server-api-contract/gen/kent/api/transcript/transcript_pb";
import { toolPresentation } from "./chatToolPresentation";
import { committedRow } from "./chatTranscriptRows";
import { transcriptMessage } from "./chatTranscript";

it.each([-1n, 0n, 255n, 2147483648n, 3221225477n, 4294967295n])(
  "preserves shell exit status %s",
  (shellExitCode) => {
    const value = create(T.ToolPresentationSchema, {
      presentation: T.ToolPresentationKind.SHELL,
      renderBehavior: T.ToolPresentationKind.SHELL,
      isShell: true,
      shellExitCode,
    });
    expect(toolPresentation(value, "exec_command")?.ShellExitCode).toBe(Number(shellExitCode));
  },
);

it.each([-1n, 0n, 255n, 2147483648n, 3221225477n, 4294967295n])(
  "preserves background notice and activity exit status %s",
  (exitCode) => {
    const id = "123e4567-e89b-42d3-a456-426614174000";
    const row = create(R.CommittedRowSchema, {
      visibility: R.EntryVisibility.ONGOING,
      integrity: R.RowIntegrity.VALID,
      locator: { eventSequence: 1n, rowOrdinal: 1 },
      row: {
        case: "notice",
        value: {
          reason: R.NoticeReason.RUNTIME_DIAGNOSTIC,
          severity: R.NoticeSeverity.INFO,
          messageType: R.NoticeMessageType.BACKGROUND_NOTICE,
          diagnostic: { code: "background_completion", detail: "completed" },
          background: { activityId: id, processId: "process-status", exitCode },
        },
      },
    });
    expect(
      committedRow(decode(R.CommittedRowSchema, encode(R.CommittedRowSchema, row))).Notice?.Background
        ?.ExitCode,
    ).toBe(Number(exitCode));
    const message = create(R.MessageSchema, {
      sequence: 2n,
      event: {
        payload: {
          case: "backgroundActivity",
          value: {
            activityId: id,
            processId: "process-status",
            ownerRunId: id,
            ownerStepId: id,
            lifecycle: R.BackgroundLifecycle.COMPLETED,
            command: "child",
            workdir: "/tmp",
            exitCode,
          },
        },
      },
    });
    const adapted = transcriptMessage(decode(R.MessageSchema, encode(R.MessageSchema, message)), "session-1");
    expect(adapted.kind).toBe("background_activity");
    if (adapted.kind === "background_activity") {
      expect(adapted.payload.ExitCode).toBe(Number(exitCode));
    }
  },
);

it("keeps absent shell exit status absent", () => {
  expect(
    toolPresentation(
      create(T.ToolPresentationSchema, {
        presentation: T.ToolPresentationKind.SHELL,
        renderBehavior: T.ToolPresentationKind.SHELL,
        isShell: true,
      }),
      "exec_command",
    )?.ShellExitCode,
  ).toBeNull();
});

it("rejects shell statuses outside the exact desktop integer range", () => {
  expect(() =>
    toolPresentation(
      create(T.ToolPresentationSchema, {
        presentation: T.ToolPresentationKind.SHELL,
        renderBehavior: T.ToolPresentationKind.SHELL,
        isShell: true,
        shellExitCode: 9007199254740992n,
      }),
      "exec_command",
    ),
  ).toThrow();
});
