import type { InputHTMLAttributes } from "react";

export const identifierInputAttributes = {
  autoCapitalize: "none",
  autoComplete: "off",
  autoCorrect: "off",
  spellCheck: false,
} satisfies Pick<
  InputHTMLAttributes<HTMLInputElement>,
  "autoCapitalize" | "autoComplete" | "autoCorrect" | "spellCheck"
>;
