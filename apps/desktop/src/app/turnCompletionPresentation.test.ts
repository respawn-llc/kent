import { expect, it } from "bun:test";
import { turnCompletionPresentation } from "./turnCompletionPresentation";

it("budgets the whole structured presentation including long titles and localized formatting", () => {
  for (const title of ["短".repeat(90), "Session", "🙂".repeat(76)]) {
    for (const separator of [": ", " — ", "："]) {
      const body = "preview ".repeat(20);
      const complete = Array.from(title + separator + body);
      const presentation = turnCompletionPresentation({ title, body, separator });
      expect(presentation.title + (presentation.body ?? "")).toBe(complete.slice(0, 77).join("") + "...");
      if (Array.from(title).length >= 77) expect(presentation.body).toBeUndefined();
    }
  }
});

it("preserves fitting structured fields without ellipsis or padding", () => {
  const parts = { title: "会話", body: "完了", separator: " / " };
  const presentation = turnCompletionPresentation(parts);
  expect(presentation.title + (presentation.body ?? "")).toBe(parts.title + parts.separator + parts.body);
});
