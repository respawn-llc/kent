import { animate, type AnimationPlaybackControls } from "motion/react";
import { motionDurationFromCSSVar, prefersReducedMotion } from "./motion";
import { textMotionSnapshot } from "./textMotionSnapshot";

interface Source {
  snapshot: HTMLElement | null;
  live: HTMLElement | null;
  expiry: ReturnType<typeof setTimeout> | null;
}

/** Destination-owned visual continuity, never an authority for submitted or pending work. */
export class SharedTextMotion {
  private readonly sources = new Map<string, Source>();
  private readonly handoffs = new Set<() => void>();
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

  arrive(ids: readonly string[], target: HTMLElement): boolean {
    let snapshot: HTMLElement | null = null;
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
    this.handoff(snapshot, target, origins);
    return true;
  }

  dispose() {
    for (const id of this.sources.keys()) this.reject(id);
    for (const finish of this.handoffs) finish();
  }

  private put(id: string, source: Source) {
    if (this.sources.size >= 32) {
      const oldest = this.sources.keys().next();
      if (!oldest.done) this.reject(oldest.value);
    }
    this.sources.set(id, source);
  }

  private handoff(source: HTMLElement, target: HTMLElement, origins: readonly HTMLElement[]) {
    if (this.handoffs.size >= 32) this.handoffs.values().next().value?.();
    const duration = Math.min(350, motionDurationFromCSSVar("--motion-text-handoff", 300)) / 1000;
    if (duration === 0) return;
    const theme = getComputedStyle(document.documentElement);
    const blur = `blur(${theme.getPropertyValue("--space-1").trim() || "4px"})`;
    const scale = `scale(${theme.getPropertyValue("--motion-text-handoff-scale").trim() || "0.92"})`;
    const layer = document.createElement("div");
    layer.style.cssText = "position:fixed;inset:0;pointer-events:none;overflow:hidden;z-index:1;";
    layer.inert = true;
    layer.append(source);
    document.body.append(layer);
    const previous = {
      opacity: target.style.opacity,
      filter: target.style.filter,
      transform: target.style.transform,
      transformOrigin: target.style.transformOrigin,
    };
    Object.assign(target.style, {
      opacity: "0",
      filter: blur,
      transform: scale,
      transformOrigin: "top left",
    });
    source.style.transformOrigin = "top left";
    const originVisibility = origins.map((element) => {
      const previous = element.style.visibility;
      element.style.visibility = "hidden";
      return { element, previous };
    });
    const controls: AnimationPlaybackControls[] = [];
    const finish = () => {
      if (!this.handoffs.delete(finish)) return;
      for (const control of controls) control.stop();
      Object.assign(target.style, previous);
      for (const { element, previous } of originVisibility) element.style.visibility = previous;
      layer.remove();
      window.removeEventListener("resize", finish);
    };
    this.handoffs.add(finish);
    window.addEventListener("resize", finish);
    // The live destination keeps ordinary reveal clipping and scrolling. Never
    // freeze a snapshot of its partially expanded bubble or fly across other rows.
    controls.push(
      animate(
        source,
        { opacity: [1, 0], filter: ["blur(0px)", blur], transform: ["scale(1)", scale] },
        { duration: duration / 2, ease: "easeInOut" },
      ),
      animate(
        target,
        { opacity: [0, 1], filter: [blur, "blur(0px)"], transform: [scale, "scale(1)"] },
        { delay: duration / 2, duration: duration / 2, ease: "easeInOut", onComplete: finish },
      ),
    );
  }
}
