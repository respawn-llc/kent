import { animate, type AnimationPlaybackControls } from "motion/react";
import { prefersReducedMotion } from "./motion";
import { measuredFontSize, textMotionSnapshot, type TextMotionSnapshot } from "./textMotionSnapshot";

interface Source {
  snapshot: TextMotionSnapshot | null;
  live: HTMLElement | null;
  expiry: ReturnType<typeof setTimeout> | null;
  deadline: number | null;
}

/** Destination-owned visual continuity, never an authority for submitted or pending work. */
export class SharedTextMotion {
  private readonly sources = new Map<string, Source>();
  private readonly handoffs = new Map<
    HTMLElement,
    Readonly<{
      finish(): void;
      freeze(): void;
      visual: HTMLElement;
      fontSize: number | null;
      deadline: number;
    }>
  >();
  private readonly consumedElements = new WeakSet<HTMLElement>();

  capture(id: string, element: HTMLElement) {
    this.reject(id);
    this.put(id, {
      snapshot: prefersReducedMotion() ? null : textMotionSnapshot(element),
      live: null,
      deadline: null,
      expiry: setTimeout(() => {
        this.reject(id);
      }, 15_000),
    });
  }

  register(id: string, element: HTMLElement): () => void {
    if (this.consumedElements.has(element)) return () => undefined;
    this.arrive([id], element);
    this.reject(id);
    const source: Source = { live: element, snapshot: null, expiry: null, deadline: null };
    this.put(id, source);
    return () => {
      if (this.sources.get(id) !== source) return;
      source.deadline = this.handoffs.get(element)?.deadline ?? null;
      source.snapshot = this.snapshot(element);
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
    let snapshot: TextMotionSnapshot | null = null;
    const origins: HTMLElement[] = [];
    let deadline: number | null = null;
    for (const id of ids) {
      const source = this.take(id, target);
      if (source === null) continue;
      deadline ??= source.deadline;
      snapshot ??= source.snapshot;
      if (source.origin !== null) origins.push(source.origin);
    }
    if (snapshot === null || prefersReducedMotion()) return false;
    this.handoff(snapshot, target, origins, deadline);
    return true;
  }

  dispose() {
    for (const id of this.sources.keys()) this.reject(id);
    for (const handoff of this.handoffs.values()) handoff.finish();
  }

  private put(id: string, source: Source) {
    if (this.sources.size >= 32) {
      const oldest = this.sources.keys().next();
      if (!oldest.done) this.reject(oldest.value);
    }
    this.sources.set(id, source);
  }

  private take(id: string, target: HTMLElement) {
    const source = this.sources.get(id);
    if (source === undefined) return null;
    this.reject(id);
    if (source.live === target) {
      this.consumedElements.add(target);
      return null;
    }
    const previous = source.live === null ? undefined : this.handoffs.get(source.live);
    const deadline = source.deadline ?? previous?.deadline ?? null;
    const snapshot = source.live === null ? source.snapshot : this.snapshot(source.live);
    if (source.live !== null) this.consumedElements.add(source.live);
    return { snapshot, deadline, origin: source.live };
  }

  private snapshot(element: HTMLElement): TextMotionSnapshot | null {
    const handoff = this.handoffs.get(element);
    if (handoff === undefined) return textMotionSnapshot(element);
    handoff.freeze();
    // The owned visual is already bounded; adopting it preserves its frozen crossfade.
    const snapshot: TextMotionSnapshot = {
      element: handoff.visual,
      rect: handoff.visual.getBoundingClientRect(),
      fontSize: handoff.fontSize,
    };
    handoff.finish();
    return snapshot;
  }

  private handoff(
    source: TextMotionSnapshot,
    target: HTMLElement,
    origins: readonly HTMLElement[],
    deadline: number | null,
  ) {
    if (this.handoffs.size >= 32) this.handoffs.values().next().value?.finish();
    const destination = textMotionSnapshot(target, "full");
    if (destination === null) return;
    const end = deadline ?? performance.now() + 350;
    const duration = Math.max(0, end - performance.now()) / 1000;
    if (duration === 0) return;
    const theme = getComputedStyle(document.documentElement);
    const blur = `blur(${theme.getPropertyValue("--space-1").trim() || "4px"})`;
    const layer = document.createElement("div");
    layer.style.cssText = "position:fixed;inset:0;pointer-events:none;overflow:hidden;z-index:1;";
    layer.inert = true;
    const visual = document.createElement("div");
    Object.assign(visual.style, {
      position: "fixed",
      left: `${String(source.rect.x)}px`,
      top: `${String(source.rect.y)}px`,
      width: `${String(Math.max(source.rect.width, destination.rect.width))}px`,
      height: `${String(Math.max(source.rect.height, destination.rect.height))}px`,
    });
    for (const snapshot of [source, destination]) {
      Object.assign(snapshot.element.style, {
        position: "absolute",
        left: "0px",
        top: "0px",
        transformOrigin: "top left",
      });
      visual.append(snapshot.element);
    }
    layer.append(visual);
    document.body.append(layer);
    const previous = target.style.visibility;
    target.style.visibility = "hidden";
    destination.element.style.opacity = "0";
    const originVisibility = origins.map((element) => {
      const previous = element.style.visibility;
      element.style.visibility = "hidden";
      return { element, previous };
    });
    const controls: AnimationPlaybackControls[] = [];
    const stop = () => {
      for (const control of controls.splice(0)) control.stop();
    };
    const freeze = () => {
      // Motion samples wall-clock time on stop, which can differ from the rendered WAAPI frame.
      const snapshots = [source.element, destination.element, visual].map((element) => {
        const style = getComputedStyle(element);
        return {
          element,
          opacity: style.opacity,
          transform: style.transform,
          scale: style.scale,
          filter: style.filter,
        };
      });
      stop();
      for (const { element, ...style } of snapshots) Object.assign(element.style, style);
    };
    const finish = () => {
      if (!this.handoffs.delete(target)) return;
      stop();
      target.style.visibility = previous;
      for (const { element, previous } of originVisibility) element.style.visibility = previous;
      layer.remove();
      window.removeEventListener("resize", finish);
    };
    this.handoffs.set(target, { finish, freeze, visual, fontSize: measuredFontSize(visual), deadline: end });
    window.addEventListener("resize", finish);
    // Capture the full destination layout, not its temporarily clipped entrance.
    // Motion owns the clock. Geometry follows ordinary scrolling without driving it.
    const fontScale =
      source.fontSize !== null && destination.fontSize !== null ? destination.fontSize / source.fontSize : 1;
    controls.push(
      animate(0, 1, {
        duration,
        ease: "easeInOut",
        onUpdate: (progress) => {
          if (!target.isConnected) return;
          const current = target.getBoundingClientRect();
          visual.style.left = `${String(source.rect.x + (current.x - source.rect.x) * progress)}px`;
          visual.style.top = `${String(source.rect.y + (current.y - source.rect.y) * progress)}px`;
        },
        onComplete: finish,
      }),
      animate(
        source.element,
        { opacity: [1, 1, 0, 0], scale: [1, fontScale, fontScale, fontScale] },
        { duration, ease: "easeInOut", times: [0, 0.12, 0.35, 1] },
      ),
      animate(
        destination.element,
        { opacity: [0, 0, 1, 1], scale: [1 / fontScale, 1 / fontScale, 1, 1] },
        { duration, ease: "easeInOut", times: [0, 0.12, 0.35, 1] },
      ),
      animate(
        visual,
        { filter: ["blur(0px)", "blur(0px)", blur, "blur(0px)"] },
        { duration, ease: "easeInOut", times: [0, 0.35, 0.7, 1] },
      ),
    );
  }
}
