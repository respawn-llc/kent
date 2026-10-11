import { composerSuggestions, dispatchComposerCommand, resolveComposerCommand } from "./composerCommands";
import { createTestServices } from "@/test-support/app-services";
import { createNameCommand } from "./nameCommand";

it("sets literal name arguments immediately without submitting model input", async () => {
  const api = createTestServices([]).api.chat;
  const setName = vi.spyOn(api, "setSessionName").mockResolvedValue();
  const steer = vi.spyOn(api, "steer");
  const queue = vi.spyOn(api, "queue");
  const command = createNameCommand({ api, existingSession: true, description: null, notify: vi.fn() });
  const resolution = resolveComposerCommand(' /name \t Ω  "review"\nsecond line  ', [command]);
  if (resolution.kind !== "direct") throw new Error("Expected direct name execution");
  const target = { kind: "session", projectID: "project-1", sessionID: "session-1" } as const;
  expect(await dispatchComposerCommand(api, target, resolution, "send")).toEqual({ kind: "local" });
  expect(setName).toHaveBeenCalledWith(target, { kind: "set", name: 'Ω  "review"\nsecond line' });
  expect(steer).not.toHaveBeenCalled();
  expect(queue).not.toHaveBeenCalled();
});

it("executes blank name arguments as Clear immediately through Queue", async () => {
  const api = createTestServices([]).api.chat;
  const setName = vi.spyOn(api, "setSessionName").mockResolvedValue();
  const queue = vi.spyOn(api, "queue");
  const command = createNameCommand({ api, existingSession: true, description: null, notify: vi.fn() });
  const resolution = resolveComposerCommand("/name \t\n ", [command]);
  if (resolution.kind !== "direct") throw new Error("Expected direct name execution");
  const target = { kind: "session", projectID: "project-1", sessionID: "session-1" } as const;
  expect(await dispatchComposerCommand(api, target, resolution, "queue")).toEqual({ kind: "local" });
  expect(setName).toHaveBeenCalledWith(target, { kind: "clear" });
  expect(queue).not.toHaveBeenCalled();
});

it("keeps unavailable commands recognized without offering them for discovery", () => {
  const notify = vi.fn();
  const command = {
    token: "/unavailable",
    aliases: [],
    description: null,
    preview: null,
    execution: { kind: "unavailable", draft: "discard", notify } as const,
  };
  expect(composerSuggestions("/", [command])).toEqual([]);
  expect(resolveComposerCommand(command.token, [command])).toEqual({
    kind: "unavailable",
    draft: "discard",
    notify,
  });
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
