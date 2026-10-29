import { ContractError } from "./errors";

export function requireTaskBoundItems(taskID: string, items: readonly Readonly<{ taskID: string }>[]): void {
  if (items.some((item) => item.taskID !== taskID)) {
    throw new ContractError("response contains an item for another task.");
  }
}
