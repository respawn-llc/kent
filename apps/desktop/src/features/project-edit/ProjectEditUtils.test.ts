import { describe, expect, it } from "vitest";

import { projectKeyErrors } from "./ProjectEditUtils";

const identity = (key: string): string => key;

describe("projectKeyErrors", () => {
  it("accepts a valid uppercase alphanumeric key", () => {
    expect(projectKeyErrors("KNT", identity)).toEqual([]);
    expect(projectKeyErrors("AB12CD34", identity)).toEqual([]);
  });

  it("rejects keys outside the 2-8 length bounds", () => {
    expect(projectKeyErrors("A", identity)).toContain("form.projectKeyLength");
    expect(projectKeyErrors("ABCDEFGHI", identity)).toContain("form.projectKeyLength");
  });

  it("rejects keys not starting with a letter", () => {
    expect(projectKeyErrors("1AB", identity)).toContain("form.projectKeyStartsWithLetter");
  });

  it("rejects disallowed symbols and whitespace", () => {
    expect(projectKeyErrors("A-B", identity)).toContain("form.projectKeySymbols");
    expect(projectKeyErrors("A B", identity)).toContain("form.noWhitespace");
  });
});
