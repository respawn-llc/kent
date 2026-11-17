import { expect, it } from "bun:test";
import { taskOutputRoot } from "./taskExecutionTarget";

it("never substitutes the source workspace for an unavailable managed output root", () => {
  expect(taskOutputRoot("head", null, "/source")).toEqual({ kind: "unavailable" });
  expect(taskOutputRoot("none", null, "/source")).toEqual({ kind: "source", path: "/source" });
  expect(taskOutputRoot("head", "/managed", "/source")).toEqual({ kind: "managed", path: "/managed" });
  expect(taskOutputRoot(undefined, null, "/source")).toEqual({ kind: "unavailable" });
});
