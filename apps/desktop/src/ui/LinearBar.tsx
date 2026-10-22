import "./linearBar.css";

export function LinearBar({
  segments,
}: Readonly<{ segments: readonly Readonly<{ id: string; percent: number }>[] }>) {
  return (
    <div className="linear-bar">
      {segments.map(({ id, percent }) => (
        <span key={id} className="linear-bar-segment" style={{ width: `${percent.toString()}%` }} />
      ))}
    </div>
  );
}
