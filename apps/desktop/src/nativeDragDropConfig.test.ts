import { describe, expect, it } from "vitest";

import tauriConfig from "../src-tauri/tauri.conf.json";

describe("Tauri drag/drop config", () => {
  it("keeps the native drag/drop handler disabled so HTML5 board DnD works", async () => {
    const config = parseObject(tauriConfig);
    const app = readObject(config, "app");
    const windows = readArray(app, "windows");
    const mainWindow = windows.find(
      (windowConfig): windowConfig is Readonly<Record<string, unknown>> =>
        isObject(windowConfig) && windowConfig.title === "Kent",
    );

    expect(mainWindow).toBeDefined();
    expect(mainWindow?.dragDropEnabled).toBe(false);
  });
});

function parseObject(value: unknown): Readonly<Record<string, unknown>> {
  if (!isObject(value)) {
    throw new Error("Tauri config root must be an object.");
  }
  return value;
}

function readObject(
  value: Readonly<Record<string, unknown>>,
  key: string,
): Readonly<Record<string, unknown>> {
  const item = value[key];
  if (!isObject(item)) {
    throw new Error(`Tauri config field ${key} must be an object.`);
  }
  return item;
}

function readArray(value: Readonly<Record<string, unknown>>, key: string): readonly unknown[] {
  const item = value[key];
  if (!Array.isArray(item)) {
    throw new Error(`Tauri config field ${key} must be an array.`);
  }
  return item;
}

function isObject(value: unknown): value is Readonly<Record<string, unknown>> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}
