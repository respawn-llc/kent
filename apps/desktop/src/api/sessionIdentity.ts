export function sessionDisplayID(sessionID: string, shortID: bigint | null): string {
  return shortID === null ? sessionID : shortID.toString();
}
