import "bun-types/test";

declare global {
  interface DesktopTestEnvironment {
    DEV: boolean;
    KENT_DEBUG?: string;
    MODE: string;
    PROD: boolean;
    VITE_APP_RPC_ENDPOINT?: string;
  }

  var __KENT_TEST_ENV__: DesktopTestEnvironment;
}

export {};
