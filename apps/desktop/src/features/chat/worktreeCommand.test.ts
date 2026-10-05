import { resolveComposerCommand } from "./composerCommands";
import { createWorktreeCommand } from "./worktreeCommand";
import { appI18n } from "@/i18n";

it.each(["/worktree", "/wt", "/wt StAtUs"])("opens the ordinary list for %s", async (text) => {
  const execute = vi.fn();
  const push = vi.fn();
  const command = resolveComposerCommand(text, [createWorktreeCommand({ execute, push, t: appI18n.t })]);
  expect(command.kind).toBe("direct");
  if (command.kind !== "direct") throw new Error("Expected direct command");
  await expect(
    command.execution.send(
      {
        kind: "session",
        sessionID: "session",
        projectID: "project",
      },
      command.invocation,
    ),
  ).resolves.toEqual({ kind: "local" });
  expect(execute).toHaveBeenCalledWith("session", { kind: "list" });
  expect(push).not.toHaveBeenCalled();
});

it.each([
  "ls",
  "unknown",
  "status extra",
  "new topic",
  "create topic",
  "leave main",
  "switch",
  "delete a b",
  "remove a b",
  "rm a b",
])("rejects %s without dispatch", async (args) => {
  const execute = vi.fn();
  const push = vi.fn();
  const command = resolveComposerCommand(`/worktree ${args}`, [
    createWorktreeCommand({ execute, push, t: appI18n.t }),
  ]);
  if (command.kind !== "direct") throw new Error("Expected direct command");
  await expect(
    command.execution.send(
      {
        kind: "session",
        sessionID: "session",
        projectID: "project",
      },
      command.invocation,
    ),
  ).resolves.toEqual({ kind: "local" });
  expect(execute).not.toHaveBeenCalled();
  expect(push).toHaveBeenCalledOnce();
  expect(push).toHaveBeenCalledWith(expect.objectContaining({ tone: "danger" }));
});

it.each([
  ["new", { kind: "create" }],
  ["CREATE", { kind: "create" }],
  ["switch \t my \n branch ", { kind: "switch", selector: "my branch" }],
  ['switch "quoted branch"', { kind: "switch", selector: '"quoted branch"' }],
  ["leave", { kind: "leave" }],
  ["delete", { kind: "delete", selector: null }],
  ["remove topic", { kind: "delete", selector: "topic" }],
  ["RM\t topic", { kind: "delete", selector: "topic" }],
  ["delete 'topic'", { kind: "delete", selector: "'topic'" }],
])("routes %s with TUI argument semantics", async (args, expected) => {
  const execute = vi.fn();
  const push = vi.fn();
  const command = resolveComposerCommand(`/wt ${args}`, [
    createWorktreeCommand({ execute, push, t: appI18n.t }),
  ]);
  if (command.kind !== "direct") throw new Error("Expected direct command");
  await command.execution.send(
    {
      kind: "session",
      sessionID: "session",
      projectID: "project",
    },
    command.invocation,
  );
  expect(execute).toHaveBeenCalledWith("session", expected);
  expect(push).not.toHaveBeenCalled();
});
