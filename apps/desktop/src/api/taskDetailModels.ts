export type TaskLiveSession = Readonly<{
  sessionID: string;
  shortID: bigint | null;
  sessionName: string | null;
  nodeDisplayName: string;
}>;
