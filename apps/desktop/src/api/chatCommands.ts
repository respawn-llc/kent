import { create } from "@app/server-api-contract";
import { PromptCommandService } from "@app/server-api-contract/gen/kent/api/prompt_command/prompt_command_pb";
import { requireProjectAttachment } from "./chatAttachment";
import { requireChatSuccess } from "./chatErrors";
import { requireChatProjectTarget, requireChatSessionID } from "./chatTarget";
import type { ChatApi } from "./chatTypes";
import type { DescriptorRpcTransport } from "./transport";

export function createChatCommandApi(transport: DescriptorRpcTransport): Pick<ChatApi, "getCommandCatalog"> {
  return {
    async getCommandCatalog(target) {
      const method = PromptCommandService.method.getCatalog;
      if (target.kind === "session") requireChatSessionID(target);
      else requireChatProjectTarget(target);
      const result =
        target.kind === "session"
          ? await transport.callDescriptorAttachedSession(
              target,
              method,
              create(method.input, { sessionId: target.sessionID }),
            )
          : await transport
              .callDescriptorAttachedProject({
                projectID: target.projectID,
                selector: target.workspace,
                method,
                createRequest: () => create(method.input),
              })
              .then((call) => {
                requireProjectAttachment(call.attachment, target);
                return call.result;
              });
      return requireChatSuccess(method, result).commands.map(({ name, preview }) => ({ name, preview }));
    },
  };
}
