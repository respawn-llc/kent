export type ResizeObserverGeometry = Readonly<{
  clientHeight?: number | undefined;
  rect?: DOMRect | undefined;
  scrollHeight?: number | undefined;
}>;

export type ResizeObserverGeometryHarness = Readonly<{
  notify(): void;
  restore(): void;
  setGeometry(element: HTMLElement, geometry: ResizeObserverGeometry): void;
}>;

export function installVirtualizedScrollGeometry(viewportHeight: number): Readonly<{
  resize(element: HTMLElement, height: number): void;
  restore(): void;
}> {
  const originalResizeObserver = globalThis.ResizeObserver;
  const observers = new Set<{
    targets: Set<Element>;
    callback: ResizeObserverCallback;
    observer: ResizeObserver;
  }>();
  globalThis.ResizeObserver = class implements ResizeObserver {
    readonly targets = new Set<Element>();
    constructor(callback: ResizeObserverCallback) {
      observers.add({ targets: this.targets, callback, observer: this });
    }
    observe(target: Element) {
      this.targets.add(target);
    }
    unobserve(target: Element) {
      this.targets.delete(target);
    }
    disconnect() {
      this.targets.clear();
    }
  };
  const clientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientHeight");
  const scrollHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollHeight");
  const scrollTo = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollTo");
  const scrollTop = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollTop");
  const offsets = new WeakMap<HTMLElement, number>();
  Object.defineProperties(HTMLElement.prototype, {
    clientHeight: {
      configurable: true,
      get: () => viewportHeight,
    },
    scrollHeight: {
      configurable: true,
      get(this: HTMLElement) {
        // The list's size container supplies geometry that jsdom cannot lay out.
        const content = this.firstElementChild;
        return content instanceof HTMLElement ? Number.parseFloat(content.style.height) || 0 : 0;
      },
    },
    scrollTo: {
      configurable: true,
      value(this: HTMLElement, options: ScrollToOptions) {
        // Smooth scrolling has not reached its destination in the opening frame.
        if (options.behavior === "smooth") return;
        this.scrollTop = options.top ?? this.scrollTop;
        requestAnimationFrame(() => this.dispatchEvent(new Event("scroll")));
      },
    },
    scrollTop: {
      configurable: true,
      get(this: HTMLElement) {
        const offset = Math.max(0, Math.min(offsets.get(this) ?? 0, this.scrollHeight - this.clientHeight));
        offsets.set(this, offset);
        return offset;
      },
      set(this: HTMLElement, offset: number) {
        offsets.set(this, Math.max(0, Math.min(offset, this.scrollHeight - this.clientHeight)));
      },
    },
  });
  return {
    resize(element, height) {
      const box = [{ inlineSize: 800, blockSize: height }];
      const entry: ResizeObserverEntry = {
        target: element,
        borderBoxSize: box,
        contentBoxSize: box,
        devicePixelContentBoxSize: box,
        contentRect: new DOMRect(0, 0, 800, height),
      };
      for (const record of observers) {
        if (record.targets.has(element)) record.callback([entry], record.observer);
      }
    },
    restore() {
      globalThis.ResizeObserver = originalResizeObserver;
      restoreDescriptor(HTMLElement.prototype, "clientHeight", clientHeight);
      restoreDescriptor(HTMLElement.prototype, "scrollHeight", scrollHeight);
      restoreDescriptor(HTMLElement.prototype, "scrollTo", scrollTo);
      restoreDescriptor(HTMLElement.prototype, "scrollTop", scrollTop);
    },
  };
}

export function installResizeObserverGeometry(): ResizeObserverGeometryHarness {
  const originalResizeObserver = globalThis.ResizeObserver;
  const originalClientHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "clientHeight");
  const originalScrollHeight = Object.getOwnPropertyDescriptor(HTMLElement.prototype, "scrollHeight");
  const originalGetBoundingClientRect = Object.getOwnPropertyDescriptor(
    HTMLElement.prototype,
    "getBoundingClientRect",
  );
  const geometries = new WeakMap<HTMLElement, ResizeObserverGeometry>();
  const observers: ControlledResizeObserver[] = [];

  globalThis.ResizeObserver = class extends ControlledResizeObserver {
    constructor(callback: ResizeObserverCallback) {
      super(callback);
      observers.push(this);
    }
  };
  Object.defineProperty(HTMLElement.prototype, "clientHeight", {
    configurable: true,
    get(this: HTMLElement) {
      return geometries.get(this)?.clientHeight ?? 0;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "scrollHeight", {
    configurable: true,
    get(this: HTMLElement) {
      return geometries.get(this)?.scrollHeight ?? 0;
    },
  });
  Object.defineProperty(HTMLElement.prototype, "getBoundingClientRect", {
    configurable: true,
    value(this: HTMLElement): DOMRect {
      const geometry = geometries.get(this);
      if (geometry?.rect !== undefined) {
        return geometry.rect;
      }
      return emptyRect();
    },
  });

  return {
    notify() {
      for (const observer of observers) {
        observer.notify();
      }
    },
    restore() {
      globalThis.ResizeObserver = originalResizeObserver;
      restoreDescriptor(HTMLElement.prototype, "clientHeight", originalClientHeight);
      restoreDescriptor(HTMLElement.prototype, "scrollHeight", originalScrollHeight);
      restoreDescriptor(HTMLElement.prototype, "getBoundingClientRect", originalGetBoundingClientRect);
    },
    setGeometry(element, geometry) {
      geometries.set(element, geometry);
    },
  };
}

class ControlledResizeObserver implements ResizeObserver {
  readonly #callback: ResizeObserverCallback;

  constructor(callback: ResizeObserverCallback) {
    this.#callback = callback;
  }

  disconnect(): void {
    return;
  }

  observe(): void {
    return;
  }

  unobserve(): void {
    return;
  }

  notify(): void {
    this.#callback([], this);
  }
}

function restoreDescriptor(
  prototype: typeof HTMLElement.prototype,
  property: "clientHeight" | "getBoundingClientRect" | "scrollHeight" | "scrollTo" | "scrollTop",
  descriptor: PropertyDescriptor | undefined,
): void {
  if (descriptor === undefined) {
    Reflect.deleteProperty(prototype, property);
  } else {
    Object.defineProperty(prototype, property, descriptor);
  }
}

function emptyRect(): DOMRect {
  return {
    bottom: 0,
    height: 0,
    left: 0,
    right: 0,
    top: 0,
    width: 0,
    x: 0,
    y: 0,
    toJSON: () => ({}),
  };
}
