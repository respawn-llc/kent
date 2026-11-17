import { expect, it } from "bun:test";
import { outputFileTarget } from "./outputFileTarget";

it("resolves relative output files only against an available execution root", () => {
  expect(outputFileTarget("../image%20#?.png", { kind: "managed", path: "/worktree" })).toEqual({
    kind: "resolvable",
    target: { path: "../image%20#?.png", basePath: "/worktree" },
  });
  expect(outputFileTarget("image.png", { kind: "source", path: "/source" })).toEqual({
    kind: "resolvable",
    target: { path: "image.png", basePath: "/source" },
  });
  expect(outputFileTarget("image.png", { kind: "unavailable" })).toEqual({ kind: "unavailable" });
  expect(outputFileTarget("/tmp/image.png", { kind: "unavailable" })).toEqual({
    kind: "resolvable",
    target: { path: "/tmp/image.png" },
  });
});
