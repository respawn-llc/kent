import { partialWorktreeDeletionError } from "@/test-support/api";
import { appI18n, initializeI18n } from "@/i18n";
import { worktreeErrorMessage } from "./worktreeErrorMessage";

beforeAll(initializeI18n);

it.each([undefined, {}])("preserves the stopping reason without explanatory blocker facts: %j", (blocked) => {
  const diagnostic = "A background process still owns this Worktree";
  const sessionCount = 50n;
  const error = partialWorktreeDeletionError({
    retargetedSessions: sessionCount,
    diagnostic,
    blocked,
  });
  const message = worktreeErrorMessage(error, appI18n.t);
  expect(message).toContain(diagnostic);
  expect(message).toContain(
    appI18n.t("chat.worktree.deletePartial", { sessionCount: sessionCount.toString() }),
  );
});
