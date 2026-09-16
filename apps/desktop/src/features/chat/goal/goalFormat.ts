export function formatGoalAge(createdAt: string, now = Date.now()): string {
  const createdAtMillis = Date.parse(createdAt);
  if (!Number.isFinite(createdAtMillis)) {
    return "0 min";
  }
  const elapsedMinutes = Math.max(0, Math.floor((now - createdAtMillis) / 60_000));
  if (elapsedMinutes < 1) {
    return "0 min";
  }
  if (elapsedMinutes < 60) {
    return `${elapsedMinutes.toString()} min`;
  }
  const totalHours = Math.floor(elapsedMinutes / 60);
  const minutes = elapsedMinutes % 60;
  if (totalHours < 24) {
    return `${totalHours.toString()}h${minutes === 0 ? "" : `${minutes.toString()}m`}`;
  }
  const days = Math.floor(totalHours / 24);
  const hours = totalHours % 24;
  return `${days.toString()}d${hours === 0 ? "" : `${hours.toString()}h`}${minutes === 0 ? "" : `${minutes.toString()}m`}`;
}

export function formatGoalSetAt(createdAt: string, now = Date.now(), locale?: string): string {
  const absolute = new Intl.DateTimeFormat(locale, {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(new Date(createdAt));
  return `${absolute} (${formatGoalAge(createdAt, now)} ago)`;
}
