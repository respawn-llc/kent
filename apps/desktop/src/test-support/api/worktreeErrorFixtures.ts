import { create } from "@app/server-api-contract";
import { DeletePartialDetailsSchema } from "@app/server-api-contract/gen/kent/api/worktree/worktree_pb";
import { RpcError, WorktreeError } from "@/api";

export function partialWorktreeDeletionError(
  input: Parameters<typeof create<typeof DeletePartialDetailsSchema>>[1],
) {
  const details = create(DeletePartialDetailsSchema, input);
  return new WorktreeError(new RpcError({ code: -32000, message: details.diagnostic, method: "delete" }), {
    kind: "delete_partial",
    details,
  });
}
