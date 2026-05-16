import { ConnectionStore } from "./connectionStore";
import type { JsonValue } from "./json";
import type { RpcSubscription, RpcTransport } from "./transport";

export type FakeRoute = Readonly<{
  method: string;
  result: unknown;
  error?: Error;
}>;

export class FakeRpcTransport implements RpcTransport {
  readonly connection = new ConnectionStore();
  readonly calls: Readonly<{ method: string; params: JsonValue }>[] = [];
  #routes = new Map<string, FakeRoute>();

  constructor(routes: readonly FakeRoute[]) {
    for (const route of routes) {
      this.#routes.set(route.method, route);
    }
    this.connection.set("connected");
  }

  async call(method: string, params: JsonValue): Promise<unknown> {
    this.calls.push({ method, params });
    const route = this.#routes.get(method);
    if (route === undefined) {
      throw new Error(`Missing fake route: ${method}`);
    }
    if (route.error !== undefined) {
      throw route.error;
    }
    return route.result;
  }

  subscribe(): RpcSubscription {
    return {
      close() {
        return;
      },
    };
  }
}
