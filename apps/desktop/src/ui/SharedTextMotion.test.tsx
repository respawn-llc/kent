import { afterEach, expect, it, vi } from "vitest";
import { SharedTextMotion } from "./SharedTextMotion";

afterEach(() => {
  document.body.replaceChildren();
  document.documentElement.style.removeProperty("--motion-text-handoff");
  vi.unstubAllGlobals();
  vi.useRealTimers();
});

function textElement(top: number) {
  const element = document.createElement("div");
  element.textContent = "A message";
  document.body.append(element);
  vi.spyOn(element, "getBoundingClientRect").mockReturnValue(new DOMRect(20, top, 240, 30));
  return element;
}

async function layoutSettled() {
  await new Promise(requestAnimationFrame);
  await new Promise(requestAnimationFrame);
}

it.each([50, 900])("keeps a tall-chat flight at 350ms with a %ims theme duration", async (duration) => {
  document.documentElement.style.setProperty("--motion-text-handoff", `${String(duration)}ms`);
  const motion = new SharedTextMotion();
  const source = textElement(window.innerHeight - 60);
  const target = textElement(20);
  motion.capture("first", source);
  source.remove();
  expect(motion.arrive(["first"], target)).toBe(true);
  expect(target).not.toBeVisible();
  await new Promise((resolve) => setTimeout(resolve, 250));
  expect(target).not.toBeVisible();
  await new Promise((resolve) => setTimeout(resolve, 150));
  expect(target).toBeVisible();
  expect(document.body.children).toHaveLength(1);
  motion.dispose();
});

it("consumes reduced-motion delivery without overlays or historical replay", () => {
  vi.stubGlobal(
    "matchMedia",
    vi.fn((query: string) => ({
      matches: query === "(prefers-reduced-motion: reduce)",
    })),
  );
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const target = textElement(100);
  motion.capture("first", source);
  expect(motion.arrive(["first"], target)).toBe(false);
  expect(target).toBeVisible();
  expect(document.body.children).toHaveLength(2);
  vi.unstubAllGlobals();
  expect(motion.arrive(["first"], target)).toBe(false);
  motion.dispose();
});

it("bounds concurrent handoffs and restores every destination when navigating away", () => {
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const targets = Array.from({ length: 40 }, (_, index) => {
    const target = textElement(100);
    const id = String(index);
    motion.capture(id, source);
    expect(motion.arrive([id], target)).toBe(true);
    return target;
  });
  for (const target of targets.slice(0, 8)) expect(target).toBeVisible();
  expect(targets.at(-1)).not.toBeVisible();
  motion.dispose();
  for (const target of targets) expect(target).toBeVisible();
  expect(document.body.children).toHaveLength(41);
});

it("hands off a captured source once by identity and restores the real text on disposal", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const target = textElement(100);
  motion.capture("source", source);
  source.remove();
  expect(motion.arrive(["unrelated"], target)).toBe(false);
  expect(motion.arrive(["source"], target)).toBe(true);
  await layoutSettled();
  expect(target).not.toBeVisible();
  expect(motion.arrive(["source"], target)).toBe(false);
  motion.dispose();
  expect(target).toBeVisible();
  expect(document.body.children).toHaveLength(1);
});

it("retains a disappearing pending source briefly, but never replays an offscreen delivery", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const unregister = motion.register("pending", source);
  unregister();
  source.remove();
  const target = textElement(-100);
  expect(motion.arrive(["pending"], target)).toBe(true);
  await layoutSettled();
  vi.spyOn(target, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 100, 240, 30));
  expect(motion.arrive(["pending"], target)).toBe(false);
  motion.dispose();
});

it("keeps automatic scrolling independent of delivery and retires a consumed pending source", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(100);
  const target = textElement(100);
  const unregister = motion.register("pending", source);
  expect(motion.arrive(["pending"], target)).toBe(true);
  await layoutSettled();
  window.dispatchEvent(new Event("scroll"));
  expect(target).not.toBeVisible();
  unregister();
  const release = motion.register("pending", source);
  expect(motion.arrive(["pending"], target)).toBe(false);
  window.dispatchEvent(new Event("resize"));
  expect(target).toBeVisible();
  expect(source).toBeVisible();
  release();
  motion.dispose();
});

it("reveals the live destination without freezing its opening layout", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(100);
  const target = textElement(-100);
  motion.capture("first", source);
  expect(motion.arrive(["first"], target)).toBe(true);
  vi.spyOn(target, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 100, 240, 30));
  await layoutSettled();
  expect(target).not.toBeVisible();
  target.append(document.createTextNode(" with newly laid out content"));
  await new Promise((resolve) => setTimeout(resolve, 350));
  expect(target).toBeVisible();
  expect(target).toHaveTextContent("A message with newly laid out content");
  expect(document.body.children).toHaveLength(2);
  motion.dispose();
});

it("expires composer snapshots and bounds the retained source count", () => {
  vi.useFakeTimers();
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const target = textElement(100);
  for (let index = 0; index < 40; index++) motion.capture(String(index), source);
  expect(motion.arrive(["0"], target)).toBe(false);
  expect(motion.arrive(["39"], target)).toBe(true);
  motion.dispose();
  motion.capture("expired", source);
  vi.advanceTimersByTime(15_001);
  expect(motion.arrive(["expired"], target)).toBe(false);
  motion.dispose();
});

it("consumes only the supplied source identity, once, and forgets rejected sources", () => {
  const motion = new SharedTextMotion();
  const source = textElement(300);
  motion.capture("first", source);
  motion.capture("second", source);
  motion.capture("not-rendered", source);
  motion.reject("first");
  const target = textElement(100);
  expect(motion.arrive(["first"], target)).toBe(false);
  expect(motion.arrive(["second"], target)).toBe(true);
  expect(motion.arrive(["second"], target)).toBe(false);
  motion.dispose();
  expect(motion.arrive(["not-rendered"], target)).toBe(false);
});
