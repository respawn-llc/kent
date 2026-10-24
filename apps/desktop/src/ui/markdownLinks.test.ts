import { describe, expect, it } from "bun:test";

import { safeExternalUrl } from "./externalLinks";
import { markdownLinkTarget } from "./markdownLinks";

describe("Markdown link targets", () => {
  it.each([
    "/Users/nek/My Project/源.ts",
    "/Users/nek/My%20Project/%E6%BA%90.ts",
    "file:///Users/nek/My%20Project/%E6%BA%90.ts",
    "file://localhost/Users/nek/My%20Project/%E6%BA%90.ts",
    "FILE:///Users/nek/My%20Project/%E6%BA%90.ts",
  ])("resolves a local file destination: %s", (destination) => {
    expect(markdownLinkTarget(destination)).toEqual({
      kind: "file",
      path: "/Users/nek/My Project/源.ts",
    });
    expect(safeExternalUrl(destination)).toBeUndefined();
  });

  it.each(["https://example.com/a?q=b#c", "http://example.com/", "mailto:nek@example.com"])(
    "preserves the external URL policy: %s",
    (url) => {
      expect(markdownLinkTarget(url)).toEqual({ kind: "external", url });
    },
  );

  it.each([
    undefined,
    "",
    "relative/file.ts",
    "../file.ts",
    "#line-1",
    "//example.com/file.ts",
    "//localhost/file.ts",
    "file://example.com/file.ts",
    "file://user:password@localhost/file.ts",
    "file://localhost:8080/file.ts",
    "file:relative.ts",
    "file:/Users/nek/file.ts",
    "file:////example.com/file.ts",
    "file:///Users/nek/file.ts#L20",
    "file:///Users/nek/file.ts?line=20",
    "file:///Users/nek/%",
    "file:///Users/nek/%FF.ts",
    "file:///Users/nek/%00.ts",
    "file:///Users/nek/%5C.ts",
    "/%2Fexample.com/file.ts",
    "/Users/nek/\nfile.ts",
    "/Users/nek/\\file.ts",
    "javascript:alert(1)",
    "data:text/html,unsafe",
    "ftp://example.com/file.ts",
    "smb://example.com/file.ts",
  ])("rejects unsupported or malformed destinations: %s", (destination) => {
    expect(markdownLinkTarget(destination)).toBeUndefined();
  });
});
