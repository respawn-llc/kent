import { composerSuggestions, resolveComposerCommand } from "./composerCommands";

it("keeps unavailable commands recognized without offering them for discovery", () => {
  const notify = vi.fn();
  const command = {
    token: "/unavailable",
    aliases: [],
    description: null,
    preview: null,
    execution: { kind: "unavailable", notify } as const,
  };
  expect(composerSuggestions("/", [command])).toEqual([]);
  expect(resolveComposerCommand(command.token, [command])).toEqual({ kind: "unavailable", notify });
});

it.each(["  ", "\t "])("recognizes the first command token after leading %j", (leading) => {
  expect(
    resolveComposerCommand(`${leading}/hidden\t arguments `, [
      {
        token: "/visible",
        aliases: ["/hidden"],
        description: null,
        preview: null,
        execution: { kind: "prompt", catalogIdentity: "prompt:command" },
      },
    ]),
  ).toEqual({
    kind: "input",
    activation: {
      kind: "command",
      catalogIdentity: "prompt:command",
      token: "/hidden",
      separatorWhitespace: "\t ",
      arguments: "arguments ",
    },
  });
});

it("preserves the exact prompt alias, whitespace and arguments for typed admission", () => {
  expect(
    resolveComposerCommand("/hidden\t \nargument ", [
      {
        token: "/visible",
        aliases: ["/hidden"],
        description: null,
        preview: null,
        execution: { kind: "prompt", catalogIdentity: "prompt:command" },
      },
    ]),
  ).toEqual({
    kind: "input",
    activation: {
      kind: "command",
      catalogIdentity: "prompt:command",
      token: "/hidden",
      separatorWhitespace: "\t \n",
      arguments: "argument ",
    },
  });
});

it.each(["/unknown text", "$ echo hello", " \t/unknown text"])(
  "keeps unregistered ordinary input %s intact",
  (text) => {
    expect(resolveComposerCommand(text, [])).toEqual({ kind: "input", activation: { kind: "text", text } });
  },
);

it.each(["", "  ", "\t "])("keeps the reserved prompt namespace a command error after %j", (leading) => {
  expect(resolveComposerCommand(`${leading}/prompt:missing arguments`, [])).toEqual({
    kind: "unknown-prompt",
    token: "/prompt:missing",
  });
});

it("hides aliases from discovery and ends discovery when arguments begin", () => {
  const commands = [
    {
      token: "/visible",
      aliases: ["/hidden"],
      description: null,
      preview: null,
      execution: { kind: "prompt", catalogIdentity: "prompt:command" } as const,
    },
  ];
  expect(composerSuggestions("/h", commands)).toEqual([]);
  expect(composerSuggestions("/v", commands)).toEqual(commands);
  expect(composerSuggestions(" \t/v", commands)).toEqual(commands);
  expect(composerSuggestions("/visible ", commands)).toEqual([]);
});

it("does not expand an exact hidden alias into a visible prefix match", () => {
  const commands = [
    {
      token: "/review",
      aliases: ["/r"],
      description: null,
      preview: null,
      execution: { kind: "prompt", catalogIdentity: "prompt:review" } as const,
    },
  ];
  expect(composerSuggestions("/r", commands)).toEqual([]);
  expect(resolveComposerCommand("/r", commands)).toEqual({
    kind: "input",
    activation: {
      kind: "command",
      catalogIdentity: "prompt:review",
      token: "/r",
      separatorWhitespace: "",
      arguments: "",
    },
  });
});
