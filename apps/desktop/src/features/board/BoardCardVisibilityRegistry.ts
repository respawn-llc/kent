export type BoardCardVisibilityRegistry = Readonly<{
  cardElementsRef: { current: ReadonlyMap<string, HTMLElement> };
  cardObserverRef: { current: IntersectionObserver | null };
  visibleCardIDsRef: { current: ReadonlySet<string> };
}>;

export function registerCardElement(
  registry: BoardCardVisibilityRegistry,
  cardID: string,
  element: HTMLElement | null,
): void {
  const { cardElementsRef, cardObserverRef, visibleCardIDsRef } = registry;
  const currentElement = cardElementsRef.current.get(cardID);
  if (currentElement !== undefined) {
    cardObserverRef.current?.unobserve(currentElement);
  }

  const nextElements = new Map(cardElementsRef.current);
  if (element === null) {
    nextElements.delete(cardID);
    visibleCardIDsRef.current = nextVisibleCardIDs(visibleCardIDsRef.current, cardID, false);
    cardElementsRef.current = nextElements;
    return;
  }

  element.dataset.boardCardMotionId = cardID;
  nextElements.set(cardID, element);
  cardElementsRef.current = nextElements;

  if (typeof IntersectionObserver === "undefined") {
    visibleCardIDsRef.current = nextVisibleCardIDs(visibleCardIDsRef.current, cardID, true);
    return;
  }

  cardObserverRef.current ??= new IntersectionObserver((entries) => {
    for (const entry of entries) {
      if (!(entry.target instanceof HTMLElement)) {
        continue;
      }
      const observedCardID = entry.target.dataset.boardCardMotionId;
      if (observedCardID === undefined) {
        continue;
      }
      visibleCardIDsRef.current = nextVisibleCardIDs(
        visibleCardIDsRef.current,
        observedCardID,
        entry.isIntersecting,
      );
    }
  });
  cardObserverRef.current.observe(element);
}

function nextVisibleCardIDs(current: ReadonlySet<string>, cardID: string, visible: boolean): ReadonlySet<string> {
  const next = new Set(current);
  if (visible) {
    next.add(cardID);
  } else {
    next.delete(cardID);
  }
  return next;
}
