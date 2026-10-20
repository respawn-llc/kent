import { expect, it } from "bun:test";
import { TranscriptWindow } from "@/app-facade";
import { page, readWireUser } from "@/test-support/transcript-window";

const inputID = "223e4567-e89b-42d3-a456-426614174000";

it("accepts independently decoded user provenance when a transcript window reopens", async () => {
  const window = new TranscriptWindow();
  expect(
    window.dispatch({
      kind: "opening-success",
      permit: window.openingPermit,
      page: page([await readWireUser(inputID)], null),
    }).kind,
  ).toBe("accepted");
  expect(
    window.dispatch({
      kind: "replace-window",
      page: page([await readWireUser(inputID)], null),
    }).kind,
  ).toBe("accepted");
  expect(window.snapshot.items).toHaveLength(1);
  expect(
    window.dispatch({
      kind: "replace-window",
      page: page([await readWireUser("323e4567-e89b-42d3-a456-426614174000")], null),
    }).kind,
  ).toBe("contract-failure");
});
