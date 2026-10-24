import { describe, expect, it } from "bun:test";
import { isDiscardableClip } from "./clip";

describe("recording no-op rules", () => {
  it("discards a flat microphone signal", () => {
    expect(isDiscardableClip({ bytes: 1000, durationMs: 2000, hasSignal: false })).toBe(true);
  });
  it("discards empty and sub-second clips but retains a full second of sound", () => {
    expect(isDiscardableClip({ bytes: 0, durationMs: 5000, hasSignal: true })).toBe(true);
    expect(isDiscardableClip({ bytes: 100, durationMs: 999, hasSignal: true })).toBe(true);
    expect(isDiscardableClip({ bytes: 100, durationMs: 0, hasSignal: true })).toBe(true);
    expect(isDiscardableClip({ bytes: 100, durationMs: 1000, hasSignal: true })).toBe(false);
  });
});
