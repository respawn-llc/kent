import { expect, it } from "bun:test";
import { appendSpeech } from "./insertion";

it("appends exact speech against current edits with only the missing blank-line separator", () => {
  expect(appendSpeech("", " 你好 \n")).toBe(" 你好 \n");
  expect(appendSpeech("latest edit", "Привет")).toBe("latest edit\n\nПривет");
  expect(appendSpeech("latest edit\n", "Hello")).toBe("latest edit\n\nHello");
  expect(appendSpeech("latest edit\n\n", "Hello")).toBe("latest edit\n\nHello");
});
