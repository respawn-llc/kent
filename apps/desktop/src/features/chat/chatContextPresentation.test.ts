import { describe, expect, it } from "vitest";
import { contextPresentation } from "./chatContextPresentation";

describe("Context presentation", () => {
  it("calculates used percentage and abbreviates token totals to one decimal", () => {
    expect(contextPresentation(12540, 32000)).toEqual({
      usedPercent: 39,
      used: "12.5k",
      window: "32.0k",
      extent: 12540 / 32000,
    });
  });
  it("rounds the used percentage directly from used tokens", () => {
    expect(contextPresentation(109, 200)?.usedPercent).toBe(55);
  });
  it("keeps over-window numbers while saturating geometry", () => {
    expect(contextPresentation(192700, 186000)).toEqual({
      usedPercent: 104,
      used: "192.7k",
      window: "186.0k",
      extent: 1,
    });
  });
  it("keeps small counts exact and formats thousands with one decimal", () => {
    expect(contextPresentation(501, 1000)?.used).toBe("501");
    expect(contextPresentation(28500, 100000)?.used).toBe("28.5k");
  });
  it("treats missing or nonpositive windows as unavailable", () => {
    expect(contextPresentation(0, null)).toBeNull();
    expect(contextPresentation(0, 0)).toBeNull();
    expect(contextPresentation(0, -1)).toBeNull();
  });
  it("handles zero used and an exactly full window", () => {
    expect(contextPresentation(0, 999)).toEqual({
      usedPercent: 0,
      used: "0",
      window: "999",
      extent: 0,
    });
    expect(contextPresentation(999, 999)).toEqual({
      usedPercent: 100,
      used: "999",
      window: "999",
      extent: 1,
    });
  });
  it("rounds abbreviated window counts to one decimal", () => {
    expect(contextPresentation(0, 999)?.window).toBe("999");
    expect(contextPresentation(0, 1000)?.window).toBe("1.0k");
    expect(contextPresentation(0, 1999)?.window).toBe("2.0k");
  });
});
