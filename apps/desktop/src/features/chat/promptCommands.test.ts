import { appI18n } from "@/i18n";
import { composerSuggestions, resolveComposerCommand } from "./composerCommands";
import { promptCommands } from "./promptCommands";

it("registers canonical built-ins once and invokes their hidden aliases without a catalog", () => {
  const commands = promptCommands([], appI18n.t);
  expect(composerSuggestions("/", commands).map((command) => command.token)).toEqual([
    "/prompt:review",
    "/prompt:init",
  ]);
  for (const name of ["review", "init"]) {
    expect(resolveComposerCommand(`/${name} src`, commands)).toMatchObject({
      kind: "input",
      activation: { kind: "command", catalogIdentity: `prompt:${name}`, token: `/${name}`, arguments: "src" },
    });
    expect(composerSuggestions(`/${name}`, commands)).toEqual([]);
  }
});

it("merges discovered built-ins once and dispatches file identities with their server previews", () => {
  const commands = promptCommands(
    [
      { name: "prompt:review", preview: "server built-in preview" },
      { name: "prompt:init", preview: "server init preview" },
      { name: "prompt:custom", preview: "user preview" },
    ],
    appI18n.t,
  );
  expect(commands.map((command) => command.token)).toEqual([
    "/prompt:review",
    "/prompt:init",
    "/prompt:custom",
  ]);
  expect(commands[2]?.preview).toBe("user preview");
  expect(resolveComposerCommand("/prompt:custom src", commands)).toMatchObject({
    kind: "input",
    activation: { kind: "command", catalogIdentity: "prompt:custom", arguments: "src" },
  });
});
