import assert from "node:assert/strict";
import test from "node:test";

import { detectOperatingSystem, selectOperatingSystemTab } from "./platform";

test("detects supported operating systems from structured platform fields", () => {
  for (const platform of ["Windows", "Win32", "Win64"]) {
    assert.equal(detectOperatingSystem({ platform }), "windows");
  }
  for (const platform of ["macOS", "MacIntel", "MacARM"]) {
    assert.equal(detectOperatingSystem({ platform }), "macos");
  }
  for (const platform of ["Linux", "Linux x86_64", "Linux aarch64", "X11"]) {
    assert.equal(detectOperatingSystem({ platform }), "linux");
  }
});

test("uses client hints before the legacy structured platform", () => {
  assert.equal(
    detectOperatingSystem({
      userAgentData: { platform: "Windows" },
      platform: "Linux x86_64",
    }),
    "windows",
  );
  assert.equal(
    detectOperatingSystem({
      userAgentData: { platform: "Unknown" },
      platform: "MacIntel",
    }),
    "macos",
  );
});

test("does not guess an operating system for mobile or unavailable platforms", () => {
  for (const platform of ["iPhone", "iPad", "Android", "Unknown", ""]) {
    assert.equal(detectOperatingSystem({ platform }), undefined);
  }
  assert.equal(detectOperatingSystem({}), undefined);
});

test("selects the detected OS initially but preserves a manual preference", () => {
  assert.equal(selectOperatingSystemTab("windows", null), "Windows");
  assert.equal(selectOperatingSystemTab("macos", null), "macOS");
  assert.equal(selectOperatingSystemTab("linux", null), "Linux");
  assert.equal(selectOperatingSystemTab("windows", "Linux"), "Linux");
  assert.equal(selectOperatingSystemTab("linux", "macOS"), "macOS");
  assert.equal(selectOperatingSystemTab("macos", "Windows"), "Windows");
  assert.equal(selectOperatingSystemTab("windows", "Unknown"), "Windows");
  assert.equal(selectOperatingSystemTab(undefined, null), undefined);
  assert.equal(selectOperatingSystemTab(undefined, "Windows"), "Windows");
});
