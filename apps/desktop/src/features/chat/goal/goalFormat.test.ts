import { formatGoalAge } from "./goalFormat";

const createdAt = "2026-09-11T10:00:00.000Z";
const createdAtMillis = Date.parse(createdAt);

describe("Goal age formatting", () => {
  it.each([
    [59_999, "0 min"],
    [60_000, "1 min"],
    [59 * 60_000, "59 min"],
    [60 * 60_000, "1h"],
    [61 * 60_000, "1h1m"],
    [24 * 60 * 60_000, "1d"],
    [25 * 60 * 60_000, "1d1h"],
    [25 * 60 * 60_000 + 2 * 60_000, "1d1h2m"],
  ])("formats %s milliseconds as %s", (elapsed, expected) => {
    expect(formatGoalAge(createdAt, createdAtMillis + elapsed)).toBe(expected);
  });
});
