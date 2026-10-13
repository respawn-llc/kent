// A visual snapshot is deliberately bounded; very large messages render normally.
const maximumTextLength = 32_768;
const maximumElements = 128;

export type TextMotionSnapshot = Readonly<{ element: HTMLElement; rect: DOMRect; fontSize: number | null }>;

export function textMotionSnapshot(
  element: HTMLElement,
  extent: "visible" | "full" = "visible",
): TextMotionSnapshot | null {
  const rect = extent === "visible" ? visibleTextRect(element) : element.getBoundingClientRect();
  if (rect === null) return null;
  if (rect.width <= 0 || rect.height <= 0) return null;
  const textarea = element instanceof HTMLTextAreaElement;
  const text = textarea ? element.value : element.textContent;
  if (text.length > maximumTextLength || element.querySelectorAll("*").length > maximumElements) return null;
  const copy = document.createElement("div");
  copy.style.cssText = "position:fixed;pointer-events:none;overflow:hidden;margin:0;";
  Object.assign(copy.style, {
    left: `${String(rect.x)}px`,
    top: `${String(rect.y)}px`,
    width: `${String(rect.width)}px`,
    height: `${String(rect.height)}px`,
  });
  copy.inert = true;
  const layout = document.createElement("div");
  const bounds = element.getBoundingClientRect();
  copyStyles(element, layout);
  Object.assign(layout.style, {
    position: "absolute",
    margin: "0",
    left: `${String(bounds.left - rect.left - element.scrollLeft)}px`,
    top: `${String(bounds.top - rect.top - element.scrollTop)}px`,
    width: `${String(bounds.width)}px`,
    height: "auto",
    maxHeight: "none",
    transform: "none",
    opacity: "1",
    filter: "none",
    visibility: "visible",
  });
  if (textarea) {
    Object.assign(layout.style, { background: "transparent", boxShadow: "none", borderColor: "transparent" });
    layout.textContent = text;
  } else {
    for (const child of element.childNodes) layout.append(child.cloneNode(true));
    const originals = element.querySelectorAll("*");
    layout.querySelectorAll("*").forEach((node, index) => {
      const original = originals[index];
      if (node instanceof HTMLElement && original instanceof HTMLElement) copyStyles(original, node);
    });
  }
  copy.append(layout);
  return {
    element: copy,
    rect,
    fontSize: measuredFontSize(element),
  };
}

function measuredFontSize(element: HTMLElement): number | null {
  const size = Number.parseFloat(getComputedStyle(element).fontSize);
  return Number.isFinite(size) && size > 0 ? size : null;
}

function copyStyles(source: HTMLElement, target: HTMLElement) {
  const style = getComputedStyle(source);
  for (const property of style) target.style.setProperty(property, style.getPropertyValue(property));
  target.removeAttribute("id");
}

function visibleTextRect(element: HTMLElement): DOMRect | null {
  if (!element.isConnected) return null;
  const bounds = element.getBoundingClientRect();
  let left = Math.max(0, bounds.left);
  let top = Math.max(0, bounds.top);
  let right = Math.min(window.innerWidth, bounds.right);
  let bottom = Math.min(window.innerHeight, bounds.bottom);
  for (let ancestor: HTMLElement | null = element; ancestor !== null; ancestor = ancestor.parentElement) {
    const style = getComputedStyle(ancestor);
    if (style.display === "none" || style.opacity === "0" || style.visibility === "hidden") return null;
    if (ancestor === element) continue;
    const clip = ancestor.getBoundingClientRect();
    if (["auto", "scroll", "hidden", "clip"].includes(style.overflowX)) {
      left = Math.max(left, clip.left);
      right = Math.min(right, clip.right);
    }
    if (["auto", "scroll", "hidden", "clip"].includes(style.overflowY)) {
      top = Math.max(top, clip.top);
      bottom = Math.min(bottom, clip.bottom);
    }
  }
  return right > left && bottom > top ? new DOMRect(left, top, right - left, bottom - top) : null;
}
