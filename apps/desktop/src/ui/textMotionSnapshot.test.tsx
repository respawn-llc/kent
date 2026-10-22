import { afterEach, expect, it, vi } from "bun:test";
import { textMotionSnapshot } from "./textMotionSnapshot";

afterEach(() => {
  document.body.replaceChildren();
  vi.restoreAllMocks();
});

it("does not duplicate element identities when capturing text containing SVG", () => {
  const source = document.createElement("div");
  const icon = document.createElementNS("http://www.w3.org/2000/svg", "svg");
  const label = document.createElement("span");
  const ids = [crypto.randomUUID(), crypto.randomUUID(), crypto.randomUUID()] as const;
  [source.id, icon.id, label.id] = ids;
  label.textContent = "A message";
  source.append(icon, label);
  document.body.append(source);
  vi.spyOn(source, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 20, 240, 30));

  const snapshot = textMotionSnapshot(source);
  expect(snapshot).not.toBeNull();
  if (snapshot === null) throw new Error("Expected a visible text snapshot.");
  source.remove();
  document.body.append(snapshot.element);

  for (const id of ids) expect(document.getElementById(id)).toBeNull();
});
