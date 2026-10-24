export type ClipFacts = Readonly<{ bytes: number; durationMs: number; hasSignal: boolean }>;

export function isDiscardableClip(clip: ClipFacts): boolean {
  return clip.bytes === 0 || clip.durationMs < 1000 || !clip.hasSignal;
}
