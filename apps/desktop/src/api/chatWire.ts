import { ContractError } from "./errors";

export function required<T>(value: T | undefined): T {
  if (value === undefined) throw new ContractError("Chat response is missing a required fact.");
  return value;
}

export function safeNumber(value: bigint): number {
  const number = Number(value);
  if (!Number.isSafeInteger(number)) throw new ContractError("Chat integer exceeds the supported range.");
  return number;
}

export function enumValue<const Value extends string | number>(
  value: number,
  values: Readonly<Partial<Record<number, Value>>>,
): Value {
  const result = values[value];
  if (result === undefined) throw new ContractError("Chat response contains an invalid enum value.");
  return result;
}
