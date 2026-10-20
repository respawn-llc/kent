import type { SessionCategory } from "./models";

export function sessionDisplayID(sessionID: string, shortID: bigint | null): string {
  return shortID === null ? sessionID : shortID.toString();
}

export function sessionDisplayTitle(
  session: Readonly<{ name: string | null; id: string; shortID: bigint | null; category: SessionCategory }>,
  subagentTitle: (id: string) => string,
): string {
  if (session.name !== null) return session.name;
  const id = sessionDisplayID(session.id, session.shortID);
  return session.category === "subagent" ? subagentTitle(id) : id;
}
