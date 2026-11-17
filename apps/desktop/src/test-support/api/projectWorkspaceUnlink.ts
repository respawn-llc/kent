import { create } from "@app/server-api-contract";
import {
  ConnectionService,
  HandshakeResultSchema,
} from "@app/server-api-contract/gen/kent/api/connection/connection_pb";
import { ProjectCatalogService } from "@app/server-api-contract/gen/kent/api/project/project_pb";
import { binaryAck, binaryAckUnvalidated } from "./rpcFrames";
import { type MockWebSocket, waitForMockWebSocketSent } from "./mockWebSocket";

export async function respondToWorkspaceUnlink(
  socket: MockWebSocket,
  input: Readonly<{
    protocolVersion: string;
    projectID: string;
    workspaceID: string;
    blocker: Readonly<{ code: string; count?: number }>;
  }>,
): Promise<void> {
  socket.open();
  await waitForMockWebSocketSent(socket, 1);
  binaryAck(socket, 0, ConnectionService.method.handshake, {
    result: create(HandshakeResultSchema, {
      outcome: {
        case: "success",
        value: {
          identity: {
            protocolVersion: input.protocolVersion,
            serverId: "server-1",
            pid: 1,
          },
        },
      },
    }),
  });
  await waitForMockWebSocketSent(socket, 2);

  const method = ProjectCatalogService.method.unlinkWorkspace;
  binaryAckUnvalidated(socket, 1, method, {
    result: create(method.output, {
      outcome: {
        case: "success",
        value: {
          projectId: input.projectID,
          workspaceId: input.workspaceID,
          blockers: [input.blocker],
        },
      },
    }),
  });
}
