export function createChatStorageFixture(): Storage {
  const values = new Map<string, string>();
  return {
    get length() {
      return values.size;
    },
    clear: () => {
      values.clear();
    },
    getItem: (key) => values.get(key) ?? null,
    setItem: (key, value) => {
      values.set(key, value);
    },
    removeItem: (key) => {
      values.delete(key);
    },
    key: (index) => Array.from(values.keys())[index] ?? null,
  };
}

export function installChatStorageFixture(): () => void {
  const previousDescriptor = Object.getOwnPropertyDescriptor(globalThis, "localStorage");
  Object.defineProperty(globalThis, "localStorage", {
    configurable: true,
    value: createChatStorageFixture(),
  });
  return () => {
    if (previousDescriptor === undefined) Reflect.deleteProperty(globalThis, "localStorage");
    else Object.defineProperty(globalThis, "localStorage", previousDescriptor);
  };
}
