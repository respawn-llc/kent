import { AppRoot } from "./app/AppRoot";
import type { AppServices } from "./app/services";

export type AppProps = Readonly<{
  services: AppServices;
}>;

export function App({ services }: AppProps) {
  return <AppRoot services={services} />;
}
