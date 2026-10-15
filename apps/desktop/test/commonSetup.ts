import { afterEach, beforeEach } from "bun:test";

const testEnvironment: DesktopTestEnvironment = {
  DEV: true,
  MODE: "test",
  PROD: false,
};
globalThis.__KENT_TEST_ENV__ = testEnvironment;

let environmentSnapshot: DesktopTestEnvironment;

beforeEach(() => {
  environmentSnapshot = { ...testEnvironment };
});

afterEach(() => {
  testEnvironment.DEV = environmentSnapshot.DEV;
  testEnvironment.MODE = environmentSnapshot.MODE;
  testEnvironment.PROD = environmentSnapshot.PROD;
  if (environmentSnapshot.KENT_DEBUG === undefined) {
    delete testEnvironment.KENT_DEBUG;
  } else {
    testEnvironment.KENT_DEBUG = environmentSnapshot.KENT_DEBUG;
  }
  if (environmentSnapshot.VITE_APP_RPC_ENDPOINT === undefined) {
    delete testEnvironment.VITE_APP_RPC_ENDPOINT;
  } else {
    testEnvironment.VITE_APP_RPC_ENDPOINT = environmentSnapshot.VITE_APP_RPC_ENDPOINT;
  }
});
