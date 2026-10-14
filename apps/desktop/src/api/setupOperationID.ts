export class SetupOperationID {
  readonly #value: string;

  private constructor(value: string) {
    this.#value = value;
  }

  static fromProto(value: string): SetupOperationID {
    return new SetupOperationID(value);
  }

  toJSONValue(): string {
    return this.#value;
  }
}

export class UUIDv4Value<Domain extends string> {
  declare private readonly domain: Domain;
  readonly #value: SetupOperationID;

  private constructor(value: SetupOperationID) {
    this.#value = value;
  }

  static fromProto<ParsedDomain extends string>(this: void, value: string): UUIDv4Value<ParsedDomain> {
    return new UUIDv4Value<ParsedDomain>(SetupOperationID.fromProto(value));
  }

  toJSONValue(): string {
    return this.#value.toJSONValue();
  }
}

export function newSetupOperationID(): SetupOperationID {
  return SetupOperationID.fromProto(crypto.randomUUID());
}
