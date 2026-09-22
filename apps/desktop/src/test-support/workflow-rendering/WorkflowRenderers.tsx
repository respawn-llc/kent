import type { ReactElement, ReactNode } from "react";
import type { WorkflowRecord } from "@/api";

export function WorkflowActionsContextMenuStub({
  children,
}: Readonly<{ children: (loading: boolean) => ReactElement }>) {
  return children(false);
}

export function WorkflowListStub({
  items,
  renderItem,
}: Readonly<{
  items: readonly WorkflowRecord[];
  renderItem: (item: WorkflowRecord) => ReactNode;
}>) {
  return (
    <>
      {items.map((item) => (
        <div key={item.id}>{renderItem(item)}</div>
      ))}
    </>
  );
}
