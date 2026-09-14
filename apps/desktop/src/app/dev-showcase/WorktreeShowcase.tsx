import { useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { useIsFetching, useIsMutating } from "@tanstack/react-query";
import { createBrowserNativeBridge } from "@app/native-bridge";
import { CreateTargetResolutionKind } from "@/api";
import { protocolVersion } from "@/api/composition";
import { useSidebarShell, type AppServices } from "@/app-facade";
import {
  createWorktreeShowcaseFixture,
  worktreeErrorFixture,
  type FixtureRequestKind,
  type PendingFixtureRequest,
  type WorktreeShowcaseConfig,
} from "@/dev-showcase/fixtures";
import { Button, TextInput } from "@/ui";
import { AppProviders } from "../AppProviders";
import { WorktreeFixtureSurface } from "./WorktreeFixtureSurface";

const requestKinds: readonly FixtureRequestKind[] = [
  "list",
  "resolve",
  "preview",
  "create",
  "switch",
  "delete",
];
const defaults: WorktreeShowcaseConfig = {
  topology: "registered",
  cleanliness: "clean",
  createKind: CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH,
  suggestion: "session-title",
  deferred: ["create", "switch", "delete"],
  scheduledDelete: false,
  retainCleanup: false,
};

export function WorktreeShowcase() {
  const [config, setConfig] = useState(defaults);
  const [pending, setPending] = useState<readonly PendingFixtureRequest[]>([]);
  const addRequest = (request: PendingFixtureRequest) => {
    setPending((entries) => [...entries, request]);
  };
  const [fixture, setFixture] = useState(() => ({
    id: crypto.randomUUID(),
    owner: createWorktreeShowcaseFixture(defaults, addRequest),
  }));
  const [dark, setDark] = useState(false);
  const [narrow, setNarrow] = useState(false);
  const [reducedMotion, setReducedMotion] = useState(false);
  useEffect(() => {
    document.documentElement.setAttribute("data-theme", dark ? "dark" : "light");
  }, [dark]);
  useEffect(() => {
    const variables = [
      "--motion-fast",
      "--motion-normal",
      "--motion-expressive",
      "--motion-morph",
      "--motion-morph-stage",
      "--motion-morph-stage-delay",
    ];
    for (const variable of variables) {
      if (reducedMotion) document.documentElement.style.setProperty(variable, "0ms");
      else document.documentElement.style.removeProperty(variable);
    }
  }, [reducedMotion]);
  const finish = (request: PendingFixtureRequest, error?: Error) => {
    setPending((entries) => entries.filter((entry) => entry.id !== request.id));
    if (error === undefined) request.succeed();
    else request.fail(error);
  };
  return (
    <main className="h-full overflow-auto bg-[var(--color-background)] p-[var(--space-4)] text-[var(--color-on-background)]">
      <h1 className="text-xl font-bold">Worktree actions — fixture only</h1>
      <p>
        No repository, model, or server is used. Apply/reset starts a fresh fixture. Release acknowledgements
        separately from target updates.
      </p>
      <div className="grid gap-[var(--space-2)]">
        <fieldset className="flex flex-wrap gap-[var(--space-2)]">
          <legend>Topology for the next reset</legend>
          {(["registered", "external", "missing", "detached", "empty"] as const).map((topology) => (
            <Button
              key={topology}
              variant={config.topology === topology ? "primary" : "secondary"}
              onClick={() => {
                setConfig({ ...config, topology });
              }}
            >
              {topology}
            </Button>
          ))}
        </fieldset>
        <fieldset className="flex flex-wrap gap-[var(--space-2)]">
          <legend>Delete preview for the next reset</legend>
          {(["clean", "dirty", "unknown"] as const).map((cleanliness) => (
            <Button
              key={cleanliness}
              variant={config.cleanliness === cleanliness ? "primary" : "secondary"}
              onClick={() => {
                setConfig({ ...config, cleanliness });
              }}
            >
              {cleanliness}
            </Button>
          ))}
          <label>
            <input
              type="checkbox"
              checked={config.scheduledDelete}
              onChange={(event) => {
                setConfig({ ...config, scheduledDelete: event.target.checked });
              }}
            />{" "}
            Scheduled deletion
          </label>
          <label>
            <input
              type="checkbox"
              checked={config.retainCleanup}
              onChange={(event) => {
                setConfig({ ...config, retainCleanup: event.target.checked });
              }}
            />{" "}
            Retain cleanup
          </label>
        </fieldset>
        <fieldset className="flex flex-wrap gap-[var(--space-2)]">
          <legend>Create resolution for the next reset</legend>
          {(
            [
              ["New branch", CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_NEW_BRANCH],
              [
                "Existing branch",
                CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_EXISTING_BRANCH,
              ],
              [
                "Detached ref",
                CreateTargetResolutionKind.WORKTREE_CREATE_TARGET_RESOLUTION_KIND_DETACHED_REF,
              ],
            ] as const
          ).map(([label, createKind]) => (
            <Button
              key={label}
              variant={config.createKind === createKind ? "primary" : "secondary"}
              onClick={() => {
                setConfig({ ...config, createKind });
              }}
            >
              {label}
            </Button>
          ))}
          <TextInput
            label="Server-provided branch suggestion (blank means absent)"
            value={config.suggestion}
            onChange={(event) => {
              setConfig({ ...config, suggestion: event.target.value });
            }}
          />
        </fieldset>
        <fieldset className="flex flex-wrap gap-[var(--space-2)]">
          <legend>Defer requests after the next reset</legend>
          {requestKinds.map((kind) => (
            <label key={kind}>
              <input
                type="checkbox"
                checked={config.deferred.includes(kind)}
                onChange={(event) => {
                  setConfig({
                    ...config,
                    deferred: event.target.checked
                      ? [...config.deferred, kind]
                      : config.deferred.filter((entry) => entry !== kind),
                  });
                }}
              />{" "}
              {kind}
            </label>
          ))}
        </fieldset>
        <div className="flex flex-wrap gap-[var(--space-2)]">
          <Button
            onClick={() => {
              setPending([]);
              setFixture({
                id: crypto.randomUUID(),
                owner: createWorktreeShowcaseFixture(config, addRequest),
              });
            }}
          >
            Apply scenario / reset
          </Button>
          <label>
            <input
              type="checkbox"
              checked={dark}
              onChange={(event) => {
                setDark(event.target.checked);
              }}
            />{" "}
            Dark
          </label>
          <label>
            <input
              type="checkbox"
              checked={narrow}
              onChange={(event) => {
                setNarrow(event.target.checked);
              }}
            />{" "}
            Narrow
          </label>
          <label>
            <input
              type="checkbox"
              checked={reducedMotion}
              onChange={(event) => {
                setReducedMotion(event.target.checked);
              }}
            />{" "}
            Reduced motion
          </label>
        </div>
        <section className="grid gap-[var(--space-2)]">
          <h2 className="font-bold">Pending fixture responses ({pending.length})</h2>
          {pending.map((request) => (
            <div key={request.id} className="flex flex-wrap items-center gap-[var(--space-2)]">
              <code>
                {request.kind}: {request.input}
              </code>
              <Button
                onClick={() => {
                  finish(request);
                }}
              >
                Succeed
              </Button>
              <Button
                onClick={() => {
                  finish(request, new Error("Fixture request failed"));
                }}
              >
                Fail
              </Button>
              {request.kind === "create"
                ? (["base_ref", "form", "setup"] as const).map((kind) => (
                    <Button
                      key={kind}
                      onClick={() => {
                        finish(request, worktreeErrorFixture(kind));
                      }}
                    >
                      Fail {kind}
                    </Button>
                  ))
                : null}
              {request.kind === "delete" ? (
                <Button
                  onClick={() => {
                    finish(request, worktreeErrorFixture("delete_precondition"));
                  }}
                >
                  Reject changed cleanliness
                </Button>
              ) : null}
              <Button
                onClick={() => {
                  finish(request, worktreeErrorFixture("blocked", "Fixture Task blocks this operation"));
                }}
              >
                Task blocked
              </Button>
            </div>
          ))}
        </section>
      </div>
      <div className="relative mt-[var(--space-4)] min-h-[640px]" style={{ maxWidth: narrow ? 390 : 1100 }}>
        <ShowcaseSession key={fixture.id} fixture={fixture.owner} />
      </div>
    </main>
  );
}

function ShowcaseSession({
  fixture,
}: Readonly<{ fixture: ReturnType<typeof createWorktreeShowcaseFixture> }>) {
  const [services] = useState<AppServices>(() => {
    const nativeBridge = createBrowserNativeBridge();
    return {
      api: fixture.api,
      nativeBridge,
      logger: {
        append: async (level, message, context = {}) =>
          nativeBridge.logging.append({
            level,
            message,
            context,
            occurredAt: new Date().toISOString(),
          }),
      },
      protocolVersion,
      endpoint: "fixture-only",
      homePath: "/repo",
      storageNamespace: null,
      debugThemeOverrideEnabled: false,
    };
  });
  useEffect(() => {
    fixture.hydrate();
  }, [fixture]);
  return (
    <AppProviders services={services}>
      <WorktreeFixtureSurface api={fixture.runtime.api}>
        <LiveControls fixture={fixture} />
      </WorktreeFixtureSurface>
    </AppProviders>
  );
}

function LiveControls({ fixture }: Readonly<{ fixture: ReturnType<typeof createWorktreeShowcaseFixture> }>) {
  const { close } = useSidebarShell();
  const fetching = useIsFetching();
  const mutating = useIsMutating();
  const [prompt, setPrompt] = useState<"question" | "approval" | null>(null);
  return (
    <div className="grid gap-[var(--space-2)]">
      <div className="flex flex-wrap gap-[var(--space-2)]">
        <Button onClick={() => close()}>Close sidebar</Button>
        <Button
          onClick={() => {
            fixture.applyTarget(true);
          }}
        >
          Apply Worktree target
        </Button>
        <Button
          onClick={() => {
            fixture.applyTarget(false);
          }}
        >
          Apply main target
        </Button>
        <Button
          onClick={() => {
            fixture.outcome("completed");
          }}
        >
          Later completed outcome
        </Button>
        <Button
          onClick={() => {
            fixture.outcome("failed");
          }}
        >
          Later failed outcome
        </Button>
        <Button
          onClick={() => {
            fixture.transport.connection.set("disconnected");
            fixture.transport.connection.set("connected");
          }}
        >
          Reconnect
        </Button>
        {(["question", "approval"] as const).map((kind) => (
          <Button
            key={kind}
            onClick={() => {
              fixture.prompt(kind);
              setPrompt(kind);
            }}
          >
            Show pending {kind}
          </Button>
        ))}
      </div>
      <p>
        List reads: {fixture.listReadCount()}; API requests: {fixture.transport.descriptorCalls.length};
        fetching: {fetching}; Query mutations pending: {mutating}.
      </p>
      {prompt === null ? null : (
        <p className="border border-[var(--color-outline)] p-[var(--space-2)]">
          Pending {prompt} fixture — must remain unchanged by Worktree actions. Production composer
          integration is separate.
        </p>
      )}
    </div>
  );
}

const root = document.getElementById("root");
if (root === null) throw new Error("Worktree showcase root is missing");
createRoot(root).render(<WorktreeShowcase />);
