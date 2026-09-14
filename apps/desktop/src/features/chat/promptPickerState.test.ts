import { describe, expect, it } from "vitest";
import { question, approval } from "@/test-support/chat-prompts";
import { emptyPickerState, transitionPicker } from "./promptPickerState";

describe("Chat prompt drafts", () => {
  it("anchors recommendations tentatively once and leaves other choices empty", () => {
    const prompts = [question("recommended", { recommendedOptionIndex: 2 }), question(), approval()];
    const { state } = transitionPicker(emptyPickerState(), prompts, { kind: "sync" });
    expect(state.current).toBe("recommended");
    expect(state.drafts.get("recommended")).toMatchObject({
      status: "tentative",
      selection: { kind: "suggested", number: 2 },
    });
    expect(state.drafts.get("question-1")?.selection).toEqual({ kind: "none" });
    expect(state.drafts.get("approval-1")?.selection).toEqual({ kind: "none" });
    const changed = transitionPicker(state, [question("recommended", { recommendedOptionIndex: 1 })], {
      kind: "sync",
    });
    expect(changed.state.drafts.get("recommended")?.selection).toEqual({ kind: "suggested", number: 2 });
  });
  it("removes drafts by identity, advances past a removed visible prompt, and admits only the earliest Step", () => {
    const first = question("first");
    const second = question("second");
    const third = question("third");
    const future = question("future", { stepID: "423e4567-e89b-42d3-a456-426614174000" });
    const prompts = [first, second, third, future];
    let state = transitionPicker(emptyPickerState(), prompts, { kind: "sync" }).state;
    expect(state.drafts.has("future")).toBe(false);
    state = transitionPicker(state, prompts, { kind: "navigate", direction: 1 }).state;
    state = transitionPicker(state, prompts, { kind: "commentary", text: "Retained" }).state;
    const removed = transitionPicker(state, [first, third, future], { kind: "sync" });
    expect(removed.state.current).toBe("third");
    expect(removed.state.drafts.has("second")).toBe(false);
    const next = transitionPicker(removed.state, [future], { kind: "sync" });
    expect([...next.state.drafts.keys()]).toEqual(["future"]);
    expect(next.effect).toBe("none");
    expect(transitionPicker(next.state, [], { kind: "sync" }).state).toEqual(emptyPickerState());
  });
  it("keeps decline irreversible while allowing circular navigation and explicit all-declined resend", () => {
    const prompts = [question("first"), approval(), question("third")];
    let state = transitionPicker(emptyPickerState(), prompts, { kind: "sync" }).state;
    state = transitionPicker(state, prompts, { kind: "decline" }).state;
    expect(state.current).toBe("approval-1");
    state = transitionPicker(state, prompts, {
      kind: "activate",
      selection: { kind: "approval", decision: "deny" },
    }).state;
    expect(state.current).toBe("third");
    state = transitionPicker(state, prompts, { kind: "navigate", direction: 1 }).state;
    expect(state.current).toBe("first");
    state = transitionPicker(state, prompts, { kind: "commentary", text: "Cannot undo" }).state;
    state = transitionPicker(state, prompts, {
      kind: "activate",
      selection: { kind: "suggested", number: 1 },
    }).state;
    expect(state.drafts.get("first")).toMatchObject({
      status: "declined",
      commentary: "",
      selection: { kind: "none" },
    });
    state = transitionPicker(state, prompts, { kind: "navigate", direction: -1 }).state;
    expect(state.current).toBe("third");
    expect(transitionPicker(state, prompts, { kind: "decline" }).effect).toBe("submit");
    const only = [question()];
    const declined = transitionPicker(emptyPickerState(), only, { kind: "decline" });
    expect(declined.effect).toBe("submit");
    expect(transitionPicker(declined.state, only, { kind: "confirm" }).effect).toBe("submit");
  });
  it.each(["neither", "freeform"] as const)("requires a nonblank %s response before confirming", (kind) => {
    const prompts = [question("first", kind === "freeform" ? { suggestions: [] } : {})];
    let state = transitionPicker(emptyPickerState(), prompts, { kind: "sync" }).state;
    const blank = transitionPicker(state, prompts, { kind: "activate", selection: { kind } });
    expect(blank.effect).toBe("focus-field");
    expect(blank.state.drafts.get("first")?.status).toBe("tentative");
    state = transitionPicker(blank.state, prompts, { kind: "commentary", text: " \n " }).state;
    expect(transitionPicker(state, prompts, { kind: "confirm" }).effect).toBe("focus-field");
    state = transitionPicker(state, prompts, { kind: "commentary", text: "My answer" }).state;
    const complete = transitionPicker(state, prompts, { kind: "confirm" });
    expect(complete.effect).toBe("submit");
    expect(complete.state.drafts.get("first")?.status).toBe("answered");
    expect(transitionPicker(complete.state, prompts, { kind: "sync" }).effect).toBe("none");
  });
  it("keeps keyboard selection tentative and confirms pointer activation including the anchored option", () => {
    const prompts = [question("first", { recommendedOptionIndex: 2 }), question("second")];
    let state = transitionPicker(emptyPickerState(), prompts, { kind: "sync" }).state;
    state = transitionPicker(state, prompts, {
      kind: "select",
      selection: { kind: "suggested", number: 1 },
    }).state;
    expect(state.current).toBe("first");
    expect(state.drafts.get("first")?.status).toBe("tentative");
    const confirmed = transitionPicker(state, prompts, {
      kind: "activate",
      selection: { kind: "suggested", number: 1 },
    });
    expect(confirmed.state.drafts.get("first")?.status).toBe("answered");
    expect(confirmed.state.current).toBe("second");
    expect(confirmed.effect).toBe("none");
    state = transitionPicker(confirmed.state, prompts, { kind: "navigate", direction: -1 }).state;
    state = transitionPicker(state, prompts, { kind: "commentary", text: "Addition" }).state;
    expect(state.drafts.get("first")?.status).toBe("tentative");
    expect(transitionPicker(state, prompts, { kind: "confirm" }).state.current).toBe("second");
  });
});
