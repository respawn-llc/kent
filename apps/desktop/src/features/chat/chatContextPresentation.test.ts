import { describe, expect, it } from "vitest";
import { contextPresentation } from "./chatContextPresentation";

describe("Context presentation", () => {
  it("calculates usage and remaining percentages from raw totals", () => {
    expect(contextPresentation(12540, 32000)).toEqual({
      usedPercent: 39,
      remainingPercent: 61,
      remaining: "19k",
      extent: 12540 / 32000,
    });
  });
  it("rounds the remaining percentage directly from remaining tokens", () => {
    expect(contextPresentation(109, 200)?.remainingPercent).toBe(46);
  });
  it("keeps over-window numbers while saturating geometry", () => {
    expect(contextPresentation(192700, 186000)).toEqual({
      usedPercent: 104,
      remainingPercent: -4,
      remaining: "-6k",
      extent: 1,
    });
  });
  it("shows exact remaining values below one thousand", () => {
    expect(contextPresentation(501, 1000)?.remaining).toBe("499");
    expect(contextPresentation(1201, 1000)?.remaining).toBe("-201");
  });
  it("treats missing or nonpositive windows as unavailable", () => {
    expect(contextPresentation(0, null)).toBeNull();
    expect(contextPresentation(0, 0)).toBeNull();
    expect(contextPresentation(0, -1)).toBeNull();
  });
  it("handles zero used and an exactly full window", () => {
    expect(contextPresentation(0, 999)).toEqual({
      usedPercent: 0,
      remainingPercent: 100,
      remaining: "999",
      extent: 0,
    });
    expect(contextPresentation(999, 999)).toEqual({
      usedPercent: 100,
      remainingPercent: 0,
      remaining: "0",
      extent: 1,
    });
  });
});
