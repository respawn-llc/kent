import {
  DndContext,
  DragOverlay,
  PointerSensor,
  pointerWithin,
  useDndContext,
  useDndMonitor,
  useDraggable,
  useDroppable,
  useSensor,
  useSensors,
  type UniqueIdentifier,
  type DropAnimation,
} from "@dnd-kit/core";
import { useCallback, type ReactNode } from "react";
import { createPortal } from "react-dom";
import { useReducedMotion } from "./motion";

export function DragDropSurface({
  children,
  onDrop,
  onCancel,
  dropAnimation,
}: Readonly<{
  children: ReactNode;
  onDrop: (targetID: UniqueIdentifier | null, rect: DOMRectReadOnly) => void;
  onCancel: (cause?: Error) => void;
  dropAnimation?: DropAnimation | null | undefined;
}>) {
  const sensors = useSensors(useSensor(PointerSensor, { activationConstraint: { distance: 6 } }));
  return (
    <DndContext
      autoScroll={false}
      collisionDetection={pointerWithin}
      onDragCancel={() => {
        onCancel();
      }}
      onDragEnd={({ active, over }) => {
        const rect = active.rect.current.translated;
        if (rect === null) {
          const error = new Error("A dropped item has no measured position.");
          onCancel(error);
          if (import.meta.env.DEV) throw error;
          return;
        }
        onDrop(over?.id ?? null, new DOMRect(rect.left, rect.top, rect.width, rect.height));
      }}
      sensors={sensors}
    >
      {children}
      <SourcePreview dropAnimation={dropAnimation} />
    </DndContext>
  );
}

export function useDragSource({
  id,
  disabled,
  onStart,
}: Readonly<{ id: string; disabled: boolean; onStart: () => void }>) {
  const source = useDraggable({ id, disabled });
  useDndMonitor({
    onDragStart: ({ active }) => {
      if (active.id === id) onStart();
    },
  });
  return { listeners: source.listeners, setNodeRef: source.setNodeRef };
}

export function useDropTarget(id: string) {
  return useDroppable({ id }).setNodeRef;
}

function SourcePreview({ dropAnimation }: Readonly<{ dropAnimation: DropAnimation | null | undefined }>) {
  const { activeNode } = useDndContext();
  const reducedMotion = useReducedMotion();
  const preview = useCallback(
    (container: HTMLDivElement | null) => {
      if (activeNode === null || container === null) return;
      // Snapshot the existing card so the overlay does not mount another interactive view.
      container.replaceChildren(activeNode.cloneNode(true));
    },
    [activeNode],
  );
  return createPortal(
    <DragOverlay
      className="pointer-events-none rounded-[var(--radius-l)] shadow-[var(--shadow-island-1)]"
      dropAnimation={reducedMotion ? null : dropAnimation}
    >
      {activeNode === null ? null : (
        <div aria-hidden className="animate-[surface-reveal_var(--motion-fast)]" ref={preview} />
      )}
    </DragOverlay>,
    document.body,
  );
}
