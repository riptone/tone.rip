import { defineConfig, type ViteUserConfig } from "vitest/config";

type TestOptions = NonNullable<ViteUserConfig["test"]>;

/**
 * The one shape every test suite in this repo had written out by hand.
 *
 * Seven `vitest.config.ts` files agreed on all of it - `include`, coverage
 * on, a text summary, thresholds present - and differed only in the numbers
 * and, for two of them, an environment. Seven copies of an agreement is how
 * the agreement quietly stops being one: a new package copies whichever
 * neighbour it happened to look at, and a change to the shared part has to
 * be made seven times or not at all.
 *
 * So the shared part lives here and the differences stay at the call site,
 * where they mean something. `thresholds` is required rather than defaulted
 * for that reason: a coverage floor is a claim about one package's tests,
 * and inheriting somebody else's number is worse than having none.
 */
export interface TestPresetOptions {
  /** Coverage floors. Required - see above. */
  thresholds: {
    statements: number;
    branches: number;
    functions: number;
    lines: number;
  };
  /**
   * Node by default. `jsdom` for the two packages that test DOM code -
   * @repo/ui's component kit and apps/web's client scripts.
   */
  environment?: "jsdom";
  /** Passed straight through; apps/web uses it to give jsdom a real URL. */
  environmentOptions?: TestOptions["environmentOptions"];
  /**
   * Extra setup files. The jsdom polyfill below is added automatically and
   * does not belong here.
   */
  setupFiles?: string[];
  /**
   * v8 everywhere except apps/api: its suite runs inside workerd, where
   * v8's coverage API is not exposed, so it needs istanbul's source
   * instrumentation instead.
   */
  provider?: "v8" | "istanbul";
  /** For apps/api's `cloudflareTest()` pool plugin, which stays at its call site. */
  plugins?: ViteUserConfig["plugins"];
}

export function defineTestConfig(options: TestPresetOptions): ViteUserConfig {
  const {
    thresholds,
    environment,
    environmentOptions,
    setupFiles,
    provider = "v8",
    plugins,
  } = options;

  /* jsdom needs a localStorage polyfill to be usable here, always - the two
     suites that ask for jsdom had both worked that out and both carried a
     byte-identical copy of the fix in their own `test/setup.ts`. Asking for
     the environment now brings the thing that makes the environment work,
     which is the only arrangement where a third jsdom suite cannot forget. */
  const resolvedSetup =
    environment === "jsdom"
      ? ["@repo/vitest-config/jsdom-setup", ...(setupFiles ?? [])]
      : setupFiles;

  return defineConfig({
    ...(plugins ? { plugins } : {}),
    test: {
      include: ["test/**/*.test.ts"],
      ...(environment ? { environment } : {}),
      ...(environmentOptions ? { environmentOptions } : {}),
      ...(resolvedSetup ? { setupFiles: resolvedSetup } : {}),
      coverage: {
        enabled: true,
        provider,
        reporter: ["text-summary"],
        thresholds,
      },
    },
  });
}
