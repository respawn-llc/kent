import { createEvent, fireEvent, render, screen } from "@testing-library/react";
import type { NativePlatform } from "@app/native-bridge";
import { describe, expect, it, mock, vi } from "bun:test";

const nativePlatform: { current: NativePlatform } = { current: "macos" };
await mock.module("./useAppServices", () => ({
  useAppServices: () => ({ nativeBridge: { capabilities: { platform: nativePlatform.current } } }),
}));

const { isTextFieldSubmitShortcut, textFieldSubmitShortcutPolicyForPlatform, useTextFieldSubmitShortcut } =
  await import("./textFieldSubmitShortcut");
type DirectProps = Readonly<{ action: (() => void) | null; available?: boolean }>;
function DirectHarness({ action, available = true }: DirectProps) {
  const onKeyDown = useTextFieldSubmitShortcut({ action, available, kind: "direct" });
  return <input aria-label="text" onKeyDown={onKeyDown} />;
}
function press(name: string, options: KeyboardEventInit, role = "textbox") {
  fireEvent.keyDown(screen.getByRole(role, { name }), { key: "Enter", ...options });
}
describe("text field submit shortcut", () => {
  it.each([
    ["macos", { metaKey: true }],
    ["windows", { ctrlKey: true }],
    ["linux", { ctrlKey: true }],
  ] as const)("submits the direct action on %s", (platform, modifier) => {
    nativePlatform.current = platform;
    const action = vi.fn();
    render(<DirectHarness action={action} />);
    press("text", modifier);
    expect(action).toHaveBeenCalledTimes(1);
  });
  it.each([
    ["macos", { key: "Enter", ctrlKey: true }],
    ["windows", { key: "Enter", metaKey: true }],
    ["macos", { key: "Enter", metaKey: true, isComposing: true }],
    ["macos", { key: "Enter", metaKey: true, shiftKey: true }],
    ["macos", { key: "Enter", metaKey: true, altKey: true }],
    ["macos", { key: "Enter", metaKey: true, ctrlKey: true }],
    ["windows", { key: "Enter", ctrlKey: true, metaKey: true }],
    ["browser", { key: "Enter", metaKey: true }],
    ["unknown", { key: "Enter", ctrlKey: true }],
  ] as const)("ignores unsupported shortcut %s", (platform, event) => {
    expect(
      isTextFieldSubmitShortcut(
        new KeyboardEvent("keydown", event),
        textFieldSubmitShortcutPolicyForPlatform(platform),
      ),
    ).toBe(false);
  });
  it.each([
    ["macos", "meta-enter"],
    ["windows", "control-enter"],
    ["linux", "control-enter"],
    ["browser", "unavailable"],
    ["unknown", "unavailable"],
  ] as const)("maps %s to its closed UI policy", (platform, policy) => {
    expect(textFieldSubmitShortcutPolicyForPlatform(platform)).toBe(policy);
  });
  it("consumes repeated and unavailable direct shortcuts", () => {
    nativePlatform.current = "macos";
    const action = vi.fn();
    const view = render(<DirectHarness action={action} />);
    press("text", { metaKey: true });
    const repeated = createEvent.keyDown(screen.getByRole("textbox", { name: "text" }), {
      key: "Enter",
      metaKey: true,
      repeat: true,
    });
    fireEvent(screen.getByRole("textbox", { name: "text" }), repeated);
    view.rerender(<DirectHarness action={action} available={false} />);
    const unavailable = createEvent.keyDown(screen.getByRole("textbox", { name: "text" }), {
      key: "Enter",
      metaKey: true,
    });
    fireEvent(screen.getByRole("textbox", { name: "text" }), unavailable);
    expect(action).toHaveBeenCalledTimes(1);
    expect([repeated.defaultPrevented, unavailable.defaultPrevented]).toEqual([true, true]);
  });
});
