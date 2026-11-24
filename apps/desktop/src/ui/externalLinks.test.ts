import { describe, expect, it } from "bun:test";
import { compactExternalUrlLabel, safeExternalUrl } from "./externalLinks";

describe("Task source labels", () => {
  it("shows GitHub issue identity without changing the opening URL", () => {
    const url = "https://github.com/owner/repo/issues/123?view=1#discussion";
    expect(compactExternalUrlLabel(url)).toBe("owner/repo#123");
    expect(safeExternalUrl(url)).toBe(url);
  });
  it("recognizes hosted and self-hosted Jira issue paths", () => {
    expect(compactExternalUrlLabel("https://acme.atlassian.net/browse/ABC-123")).toBe("acme · ABC-123");
    expect(compactExternalUrlLabel("https://jira.example.com/jira/browse/ABC-123")).toBe(
      "jira.example.com · ABC-123",
    );
  });
  it("uses the Linear workspace and issue key", () => {
    expect(compactExternalUrlLabel("https://linear.app/team/issue/ABC-123/title")).toBe("team · ABC-123");
  });
  it("shows Confluence path titles or page IDs without fetching", () => {
    for (const [url, expected] of [
      ["https://acme.atlassian.net/wiki/spaces/ENG/pages/12345/Design+notes", "acme · Design notes"],
      ["https://confluence.example.com/spaces/ENG/pages/12345", "confluence.example.com · Page 12345"],
      [
        "https://confluence.example.com/confluence/pages/viewpage.action?pageId=12345",
        "confluence.example.com · Page 12345",
      ],
      ["https://acme.atlassian.net/wiki/display/ENG/Design%20notes", "acme · Design notes"],
      ["https://acme.atlassian.net/wiki/spaces/ENG/pages/12345/C%2B%2B+notes", "acme · C++ notes"],
    ] as const) {
      expect(compactExternalUrlLabel(url)).toBe(expected);
    }
  });
  it("recognizes GitLab issues on public and self-hosted repositories", () => {
    expect(compactExternalUrlLabel("https://gitlab.com/group/project/-/issues/123")).toBe(
      "group/project#123",
    );
    expect(compactExternalUrlLabel("https://gitlab.example.com/group/subgroup/project/-/issues/123")).toBe(
      "group/subgroup/project#123",
    );
  });
  it("shows Azure DevOps work items in both hosted URL forms", () => {
    expect(compactExternalUrlLabel("https://dev.azure.com/org/project/_workitems/edit/123")).toBe(
      "project#123",
    );
    expect(compactExternalUrlLabel("https://org.visualstudio.com/project/_workitems/edit/123")).toBe(
      "project#123",
    );
  });
  it("recognizes YouTrack issue paths and preserves ordinary source behavior", () => {
    expect(compactExternalUrlLabel("https://acme.youtrack.cloud/issue/ABC-123/title")).toBe("acme · ABC-123");
    expect(compactExternalUrlLabel("https://www.example.com/other")).toBe("example.com");
    expect(compactExternalUrlLabel("plain source text")).toBe("plain source text");
    expect(compactExternalUrlLabel("https://github.com/owner/repo/issues/no")).toBe("github.com");
    expect(safeExternalUrl("javascript:alert(1)")).toBeUndefined();
    expect(safeExternalUrl("plain source text")).toBeUndefined();
  });
});
