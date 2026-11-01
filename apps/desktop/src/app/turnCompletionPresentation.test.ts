import { expect, it } from "bun:test";
import { turnCompletionPresentation } from "./turnCompletionPresentation";

it("bounds the title without consuming the assistant preview", () => {
  const body = "preview ".repeat(20);
  for (const title of ["短".repeat(90), "Session", "🙂".repeat(90)]) {
    const presentation = turnCompletionPresentation({ title, body });
    expect(Array.from(presentation.title).length).toBeLessThanOrEqual(80);
    expect(presentation.body).toBe(Array.from(body).slice(0, 77).join("") + "...");
    if (Array.from(title).length > 80) {
      expect(presentation.title).toBe(Array.from(title).slice(0, 77).join("") + "...");
    } else {
      expect(presentation.title).toBe(title);
    }
  }
});

it("preserves fitting structured fields without ellipsis or padding", () => {
  const parts = { title: "会話", body: "完了" };
  expect(turnCompletionPresentation(parts)).toEqual(parts);
});

it("omits the preview when Markdown contains no visible text", () => {
  expect(turnCompletionPresentation({ title: "会話", body: "" }).body).toBeUndefined();
});
