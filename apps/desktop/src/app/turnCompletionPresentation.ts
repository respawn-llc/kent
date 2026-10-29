export function turnCompletionPresentation({
  title,
  body,
}: Readonly<{ title: string; body: string }>): Readonly<{ title: string; body?: string }> {
  return { title: truncate(title), ...(body.length === 0 ? {} : { body: truncate(body) }) };
}

function truncate(value: string): string {
  const characters = Array.from(value);
  return characters.length <= 80 ? value : characters.slice(0, 77).join("") + "...";
}
