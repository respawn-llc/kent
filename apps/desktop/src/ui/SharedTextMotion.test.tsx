import { afterEach, expect, it, vi } from "vitest";
import { SharedTextMotion } from "./SharedTextMotion";

afterEach(() => {
  document.body.replaceChildren();
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

it("moves a captured source once by identity and restores the real text on disposal", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const target = textElement(100);
  motion.capture("source", source);
  source.remove();
  expect(motion.arrive(["unrelated"], target, target)).toBe(false);
  expect(motion.arrive(["source"], target, target)).toBe(true);
  await layoutSettled();
  expect(target.style.visibility).toBe("hidden");
  expect(motion.arrive(["source"], target, target)).toBe(false);
  motion.dispose();
  expect(target.style.visibility).toBe("");
  expect(document.body.children).toHaveLength(1);
});

it("retains a disappearing pending source briefly, but never replays an offscreen delivery", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const unregister = motion.register("pending", source);
  unregister();
  source.remove();
  const target = textElement(-100);
  expect(motion.arrive(["pending"], target, target)).toBe(true);
  await layoutSettled();
  expect(target.style.visibility).toBe("");
  vi.spyOn(target, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 100, 240, 30));
  expect(motion.arrive(["pending"], target, target)).toBe(false);
  motion.dispose();
});

it("keeps automatic scrolling independent of delivery and retires a consumed pending source", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(100);
  const target = textElement(100);
  const unregister = motion.register("pending", source);
  expect(motion.arrive(["pending"], target, target)).toBe(true);
  await layoutSettled();
  window.dispatchEvent(new Event("scroll"));
  expect(target.style.visibility).toBe("hidden");
  unregister();
  const release = motion.register("pending", source);
  expect(motion.arrive(["pending"], target, target)).toBe(false);
  window.dispatchEvent(new Event("resize"));
  expect(target.style.visibility).toBe("");
  expect(source.style.visibility).toBe("");
  release();
  motion.dispose();
});

it("waits for committed layout before measuring its destination", async () => {
  const motion = new SharedTextMotion();
  const source = textElement(100);
  const target = textElement(-100);
  motion.capture("first", source);
  expect(motion.arrive(["first"], target, target)).toBe(true);
  vi.spyOn(target, "getBoundingClientRect").mockReturnValue(new DOMRect(20, 100, 240, 30));
  await layoutSettled();
  expect(target.style.visibility).toBe("hidden");
  motion.dispose();
});

it("expires composer snapshots and bounds the retained source count", () => {
  vi.useFakeTimers();
  const motion = new SharedTextMotion();
  const source = textElement(300);
  const target = textElement(100);
  for (let index = 0; index < 40; index++) motion.capture(String(index), source);
  expect(motion.arrive(["0"], target, target)).toBe(false);
  expect(motion.arrive(["39"], target, target)).toBe(true);
  motion.dispose();
  motion.capture("expired", source);
  vi.advanceTimersByTime(15_001);
  expect(motion.arrive(["expired"], target, target)).toBe(false);
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
  expect(motion.arrive(["first"], target, target)).toBe(false);
  expect(motion.arrive(["second"], target, target)).toBe(true);
  expect(motion.arrive(["second"], target, target)).toBe(false);
  motion.dispose();
  expect(motion.arrive(["not-rendered"], target, target)).toBe(false);
});
