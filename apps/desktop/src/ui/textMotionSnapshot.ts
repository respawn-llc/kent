export type TextMotionSnapshot = Readonly<{ element: HTMLElement; rect: DOMRect }>;

// A visual snapshot is deliberately bounded; very large messages render normally.
const maximumTextLength = 32_768;
const maximumElements = 128;

export function textMotionSnapshot(element: HTMLElement): TextMotionSnapshot | null {
  const rect = visibleTextRect(element);
  if (rect === null) return null;
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
  return { element: copy, rect };
}

function copyStyles(source: HTMLElement, target: HTMLElement) {
  const style = getComputedStyle(source);
  for (const property of style) target.style.setProperty(property, style.getPropertyValue(property));
  target.removeAttribute("id");
}

type MovingBubble = Readonly<{ element: HTMLElement; offset: number }>;

export function visibleTextRect(element: HTMLElement, movingBubble?: MovingBubble): DOMRect | null {
  if (!element.isConnected) return null;
  const bounds = element.getBoundingClientRect();
  const translation = movingBubble === undefined ? 0 : movingBubble.offset;
  let left = Math.max(0, bounds.left);
  let top = Math.max(0, bounds.top - translation);
  let right = Math.min(window.innerWidth, bounds.right);
  let bottom = Math.min(window.innerHeight, bounds.bottom - translation);
  for (let ancestor: HTMLElement | null = element; ancestor !== null; ancestor = ancestor.parentElement) {
    const style = getComputedStyle(ancestor);
    if (hidden(ancestor, style, element, movingBubble)) return null;
    if (ancestor === element) continue;
    const clip = ancestor.getBoundingClientRect();
    if (["auto", "scroll", "hidden", "clip"].includes(style.overflowX)) {
      left = Math.max(left, clip.left);
      right = Math.min(right, clip.right);
    }
    if (["auto", "scroll", "hidden", "clip"].includes(style.overflowY)) {
      const offset = movingBubble?.element.contains(ancestor) === true ? movingBubble.offset : 0;
      top = Math.max(top, clip.top - offset);
      bottom = Math.min(bottom, clip.bottom - offset);
    }
  }
  return right > left && bottom > top ? new DOMRect(left, top, right - left, bottom - top) : null;
}

function hidden(
  ancestor: HTMLElement,
  style: CSSStyleDeclaration,
  text: HTMLElement,
  bubble: MovingBubble | undefined,
) {
  if (style.display === "none") return true;
  if (style.opacity === "0" && ancestor !== bubble?.element) return true;
  return style.visibility === "hidden" && !(ancestor === text && bubble !== undefined);
}
