import type { ELK } from "./elkjs-types";

type ElkConstructor = new () => ELK;

declare const ElkConstructor: ElkConstructor;

export default ElkConstructor;
