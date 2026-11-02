import { afterEach, beforeEach, expect, it } from "bun:test";

import {
  readLastProjectRoute,
  writeLastProjectContentTab,
  writeLastProjectRoute,
  projectOpeningTab,
} from "./projectRoutePersistence";

const serverA = { kind: "server-address", identity: "ws://localhost:1000/rpc" } as const;
const serverB = { kind: "server-address", identity: "ws://localhost:2000/rpc" } as const;
const canonicalWorkflowID = "7e8d24d2-8a98-4dcf-a197-6214db1cb3c0";
const originalLocalStorage = Object.getOwnPropertyDescriptor(globalThis, "localStorage");

beforeEach(() => {
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: new TestStorage(),
  });
});

afterEach(() => {
  if (originalLocalStorage === undefined) {
    Reflect.deleteProperty(globalThis, "localStorage");
    return;
  }
  Object.defineProperty(globalThis, "localStorage", originalLocalStorage);
});

it("writes a canonical workflow selector unchanged", () => {
  writeLastProjectRoute(serverA, {
    kind: "workflow_board",
    projectId: "project-1",
    workflowId: canonicalWorkflowID,
  });

  expect(readLastProjectRoute(serverA)).toEqual({
    kind: "workflow_board",
    projectId: "project-1",
    workflowId: canonicalWorkflowID,
  });
});

it("writes a Home Project destination unchanged", () => {
  writeLastProjectRoute(serverA, { kind: "home_project", projectId: "project-1" });

  expect(readLastProjectRoute(serverA)).toEqual({
    kind: "home_project",
    projectId: "project-1",
  });
});

it("remembers each Project tab per server, with Tasks for unseen Projects", () => {
  writeLastProjectContentTab(serverA, "project-1", "subagents");
  writeLastProjectContentTab(serverA, "project-2", "sessions");
  writeLastProjectContentTab(serverB, "project-1", "tasks");
  expect(projectOpeningTab(serverA, "project-1", "lastUsed", null)).toBe("subagents");
  expect(projectOpeningTab(serverA, "project-2", "lastUsed", null)).toBe("sessions");
  expect(projectOpeningTab(serverB, "project-1", "lastUsed", null)).toBe("tasks");
  expect(projectOpeningTab(serverB, "unseen", "lastUsed", null)).toBe("tasks");
});

it("honors explicit destinations before Project opening overrides", () => {
  writeLastProjectContentTab(serverA, "project-1", "subagents");
  expect(projectOpeningTab(serverA, "project-1", "tasks", null)).toBe("tasks");
  expect(projectOpeningTab(serverA, "project-1", "sessions", null)).toBe("sessions");
  expect(projectOpeningTab(serverA, "project-1", "tasks", "sessions")).toBe("sessions");
  expect(projectOpeningTab(serverA, "project-1", "sessions", "subagents")).toBe("subagents");
});

class TestStorage implements Storage {
  #entries = new Map<string, string>();

  get length(): number {
    return this.#entries.size;
  }

  clear(): void {
    this.#entries.clear();
  }

  getItem(key: string): string | null {
    return this.#entries.get(key) ?? null;
  }

  key(index: number): string | null {
    return [...this.#entries.keys()][index] ?? null;
  }

  removeItem(key: string): void {
    this.#entries.delete(key);
  }

  setItem(key: string, value: string): void {
    this.#entries.set(key, value);
  }
}
