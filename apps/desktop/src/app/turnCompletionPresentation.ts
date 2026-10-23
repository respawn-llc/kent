export function turnCompletionPresentation({
  title,
  body,
  separator,
}: Readonly<{ title: string; body: string; separator: string }>): Readonly<{ title: string; body?: string }> {
  const heading = Array.from(title + separator);
  const preview = Array.from(body);
  if (heading.length + preview.length <= 80) return { title: heading.join(""), body };
  if (heading.length >= 77) return { title: heading.slice(0, 77).join("") + "..." };
  return { title: heading.join(""), body: preview.slice(0, 77 - heading.length).join("") + "..." };
}
