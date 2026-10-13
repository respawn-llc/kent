import { animate, type AnimationPlaybackControls } from "motion/react";
import { motionDurationFromCSSVar, prefersReducedMotion } from "./motion";
import { textMotionSnapshot, visibleTextRect, type TextMotionSnapshot } from "./textMotionSnapshot";

interface Source {
  snapshot: TextMotionSnapshot | null;
  live: HTMLElement | null;
  expiry: ReturnType<typeof setTimeout> | null;
}

/** Destination-owned visual continuity, never an authority for submitted or pending work. */
export class SharedTextMotion {
  private readonly sources = new Map<string, Source>();
  private readonly flights = new Set<() => void>();
  private readonly consumedElements = new WeakSet<HTMLElement>();

  capture(id: string, element: HTMLElement) {
    this.reject(id);
    this.put(id, {
      snapshot: prefersReducedMotion() ? null : textMotionSnapshot(element),
      live: null,
      expiry: setTimeout(() => {
        this.reject(id);
      }, 15_000),
    });
  }

  register(id: string, element: HTMLElement): () => void {
    if (this.consumedElements.has(element)) return () => undefined;
    this.reject(id);
    const source: Source = { live: element, snapshot: null, expiry: null };
    this.put(id, source);
    return () => {
      if (this.sources.get(id) !== source) return;
      source.snapshot = textMotionSnapshot(element);
      source.live = null;
      source.expiry = setTimeout(() => {
        this.reject(id);
      }, 15_000);
    };
  }

  reject(id: string) {
    const source = this.sources.get(id);
    if (source?.expiry != null) clearTimeout(source.expiry);
    this.sources.delete(id);
  }

  hasSource(ids: readonly string[]): boolean {
    return ids.some((id) => this.sources.has(id));
  }

  arrive(ids: readonly string[], target: HTMLElement, bubble: HTMLElement): boolean {
    let snapshot: TextMotionSnapshot | null = null;
    const origins: HTMLElement[] = [];
    for (const id of ids) {
      const source = this.sources.get(id);
      if (source !== undefined) {
        snapshot ??= source.live === null ? source.snapshot : textMotionSnapshot(source.live);
        if (source.live !== null) {
          origins.push(source.live);
          this.consumedElements.add(source.live);
        }
        this.reject(id);
      }
    }
    if (snapshot === null || prefersReducedMotion()) return false;
    const origin = snapshot;
    if (this.flights.size >= 32) this.flights.values().next().value?.();
    let frame = requestAnimationFrame(() => {
      frame = requestAnimationFrame(() => {
        this.flights.delete(cancel);
        const destination = textMotionSnapshot(target);
        if (destination !== null) this.fly(origin, destination, { target, bubble, origins });
      });
    });
    const cancel = () => {
      cancelAnimationFrame(frame);
      this.flights.delete(cancel);
    };
    this.flights.add(cancel);
    return true;
  }

  dispose() {
    for (const id of this.sources.keys()) this.reject(id);
    for (const finish of this.flights) finish();
  }

  private put(id: string, source: Source) {
    if (this.sources.size >= 32) {
      const oldest = this.sources.keys().next();
      if (!oldest.done) this.reject(oldest.value);
    }
    this.sources.set(id, source);
  }

  private fly(
    source: TextMotionSnapshot,
    destination: TextMotionSnapshot,
    {
      target,
      bubble,
      origins,
    }: Readonly<{ target: HTMLElement; bubble: HTMLElement; origins: readonly HTMLElement[] }>,
  ) {
    if (this.flights.size >= 32) this.flights.values().next().value?.();
    const duration = motionDurationFromCSSVar("--motion-expressive", 700) / 1000;
    const bubbleDuration = motionDurationFromCSSVar("--motion-fast", 140) / 1000;
    const layer = document.createElement("div");
    layer.style.cssText = "position:fixed;inset:0;pointer-events:none;overflow:hidden;z-index:1;";
    layer.inert = true;
    layer.append(source.element, destination.element);
    document.body.append(layer);
    const visibility = target.style.visibility;
    target.style.visibility = "hidden";
    const originVisibility = origins.map((element) => {
      const previous = element.style.visibility;
      element.style.visibility = "hidden";
      return { element, previous };
    });
    const controls: AnimationPlaybackControls[] = [];
    const finish = () => {
      if (!this.flights.delete(finish)) return;
      for (const control of controls) control.stop();
      target.style.visibility = visibility;
      for (const { element, previous } of originVisibility) element.style.visibility = previous;
      bubble.style.removeProperty("transform");
      bubble.style.removeProperty("opacity");
      layer.remove();
      window.removeEventListener("resize", finish);
    };
    this.flights.add(finish);
    window.addEventListener("resize", finish);
    const width = target.getBoundingClientRect().width;
    let bubbleOffset = Math.max(0, window.innerHeight - bubble.getBoundingClientRect().top);
    bubble.style.transform = `translateY(${String(bubbleOffset)}px)`;
    // Both layouts keep their measured width throughout. A single masked crossfade
    // changes wrapping/Markdown near the midpoint without reshuffling moving lines.
    controls.push(
      animate(bubbleOffset, 0, {
        duration: bubbleDuration,
        ease: "easeIn",
        onUpdate: (offset) => {
          bubbleOffset = offset;
          bubble.style.transform = `translateY(${String(offset)}px)`;
        },
      }),
      animate(bubble, { opacity: [0, 1] }, { duration: bubbleDuration, ease: "easeIn" }),
      animate(0, 1, {
        duration,
        ease: "easeInOut",
        onUpdate: (progress) => {
          const current = visibleTextRect(target, { element: bubble, offset: bubbleOffset });
          if (current === null || target.getBoundingClientRect().width !== width) {
            finish();
            return;
          }
          // Follow automatic tail scrolling without owning or changing the scroll position.
          const x = source.rect.x + (current.x - source.rect.x) * progress;
          const y = source.rect.y + (current.y - source.rect.y) * progress;
          for (const element of [source.element, destination.element]) {
            element.style.left = `${String(x)}px`;
            element.style.top = `${String(y)}px`;
          }
        },
        onComplete: finish,
      }),
      animate(
        source.element,
        { opacity: [1, 1, 0, 0] },
        { duration, ease: "easeInOut", times: [0, 0.4, 0.55, 1] },
      ),
      animate(
        destination.element,
        { opacity: [0, 0, 1, 1] },
        { duration, ease: "easeInOut", times: [0, 0.45, 0.6, 1] },
      ),
    );
  }
}
