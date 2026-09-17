import { useCallback, useMemo, useRef, useState, type RefObject } from "react";
import {
  ChatRuntimeProvider,
  SidebarRootOwner,
  useAppServices,
  createRefreshOpenWorktreeList,
  useSidebarShell,
  useStatusController,
  worktreeTransitionOutcomeHandler,
  type AppServices,
} from "@/app-facade";
import type { ComposerCommand } from "@/features/chat";
import {
  ChatComposer,
  ChatComposerSurface,
  WorktreeCommands,
  WorktreeControl,
  useChatComposer,
} from "@/features/chat";
import { errorMessage, type ChatSessionTarget as SessionTarget } from "@/api";
import { Button, Spinner, fieldInputClassName } from "@/ui";
import { useQueryClient } from "@tanstack/react-query";
import { useTranslation } from "react-i18next";
import { z } from "zod";
import { AppProviders } from "./AppProviders";
import { SidebarProvider } from "./sidebarProvider";
import { SidebarHost } from "./sidebar";
import { sidebarDestinationPolicy } from "./sidebarDestinationPolicy";
import { createDefaultAppServices } from "./startup/appEnvironment";
import { worktreeShowcaseServices, WorktreeShowcaseControls } from "./worktreeShowcaseServices";

export function WorktreeShowcaseView() {
  const [connection, setConnection] = useState<Readonly<{
    services: AppServices;
    target: SessionTarget;
  }> | null>(null);
  const [pending, setPending] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [controls] = useState(() => new WorktreeShowcaseControls());
  return (
    <section className="grid gap-[var(--space-3)]">
      <h2>Worktree commands</h2>
      <p>
        Connect to a disposable QA Session. Uses appRpcEndpoint from this page URL. Live Create and Delete
        change real files. Model input is disabled.
      </p>
      {connection === null ? (
        <form
          className="grid gap-[var(--space-2)]"
          onSubmit={(event) => {
            event.preventDefault();
            const data = new FormData(event.currentTarget);
            const field = (name: string) => {
              return z.string().trim().min(1).parse(data.get(name));
            };
            const target: SessionTarget = {
              projectID: field("project"),
              workspace: { workspaceID: field("workspace") },
              sessionID: field("session"),
            };
            setPending(true);
            void createDefaultAppServices()
              .then(
                (base) => {
                  setConnection({ services: worktreeShowcaseServices(base, controls), target });
                },
                (cause: unknown) => {
                  setError(errorMessage(cause));
                },
              )
              .finally(() => {
                setPending(false);
              });
          }}
        >
          {["project", "workspace", "session"].map((name) => (
            <label key={name}>
              {name}
              <input required name={name} className={fieldInputClassName} />
            </label>
          ))}
          <Button type="submit">Connect</Button>
          {pending ? <Spinner /> : null}
          {error === null ? null : <p>{error}</p>}
        </form>
      ) : (
        <>
          <FixtureControls controls={controls} />
          <AppProviders services={connection.services}>
            <SidebarProvider policy={sidebarDestinationPolicy}>
              <SidebarRootOwner>
                <div className="relative min-h-[600px]">
                  <SessionSurface target={connection.target} />
                  <SidebarHost />
                </div>
              </SidebarRootOwner>
            </SidebarProvider>
          </AppProviders>
        </>
      )}
    </section>
  );
}

function FixtureControls({ controls }: Readonly<{ controls: WorktreeShowcaseControls }>) {
  const [modelSubmissions, setModelSubmissions] = useState(controls.modelSubmissions());
  return (
    <div className="flex flex-wrap items-center gap-[var(--space-3)]">
      <label>
        Request delay (ms){" "}
        <input
          type="number"
          min={0}
          defaultValue={0}
          onChange={(event) => {
            controls.update({ delay: z.number().nonnegative().parse(event.currentTarget.valueAsNumber) });
          }}
        />
      </label>
      <label>
        <input
          type="checkbox"
          onChange={(event) => {
            controls.update({ switchFailure: event.currentTarget.checked });
          }}
        />
        Reject Switch
      </label>
      <label>
        <input
          type="checkbox"
          onChange={(event) => {
            controls.update({ previewFailure: event.currentTarget.checked });
          }}
        />
        Reject preview
      </label>
      <label>
        Delete response{" "}
        <select
          defaultValue="live"
          onChange={(event) => {
            switch (event.currentTarget.value) {
              case "live":
              case "success":
              case "warning":
              case "blocked":
              case "precondition":
                controls.update({ deleteResult: event.currentTarget.value });
            }
          }}
        >
          <option value="live">Live (destructive)</option>
          <option value="success">Success fixture (no deletion)</option>
          <option value="warning">Cleanup warning fixture (no deletion)</option>
          <option value="blocked">Blocked at confirmation</option>
          <option value="precondition">Clean-to-dirty rejection</option>
        </select>
      </label>
      <Button
        onClick={() => {
          setModelSubmissions(controls.modelSubmissions());
        }}
      >
        Read model submission count: {modelSubmissions}
      </Button>
    </div>
  );
}

function SessionSurface({ target }: Readonly<{ target: SessionTarget }>) {
  const { api, logger } = useAppServices();
  const client = useQueryClient();
  const { currentSurface } = useSidebarShell();
  const { push } = useStatusController();
  const { t } = useTranslation();
  const [newChat, setNewChat] = useState(false);
  const editorRef = useRef<HTMLTextAreaElement>(null);
  const focusComposer = useCallback(() => editorRef.current?.focus(), []);
  const host = useMemo(
    () => ({
      logger,
      onWorktreeTransitionOutcome: worktreeTransitionOutcomeHandler(
        createRefreshOpenWorktreeList(client, api, currentSurface),
        target.sessionID,
        push,
        t,
      ),
    }),
    [api, client, currentSurface, logger, push, t, target.sessionID],
  );
  return (
    <ChatRuntimeProvider api={api} target={target} host={host}>
      <Button
        onClick={() => {
          setNewChat((value) => !value);
        }}
      >
        {newChat ? "Use existing Session" : "Use lazy New Chat"}
      </Button>
      <WorktreeCommands sessionID={newChat ? null : target.sessionID} focusComposer={focusComposer}>
        {(commands) => (
          <Composer
            key={newChat ? "new" : target.sessionID}
            target={target}
            newChat={newChat}
            commands={commands}
            editorRef={editorRef}
          />
        )}
      </WorktreeCommands>
      {newChat ? null : <WorktreeControl sessionID={target.sessionID} />}
    </ChatRuntimeProvider>
  );
}

function Composer({
  target,
  newChat,
  commands,
  editorRef,
}: Readonly<{
  target: SessionTarget;
  newChat: boolean;
  commands: readonly ComposerCommand[];
  editorRef: RefObject<HTMLTextAreaElement | null>;
}>) {
  const composer = useChatComposer({
    ...(newChat
      ? {
          kind: "new_chat" as const,
          projectID: target.projectID,
          workspace: target.workspace,
          submission: {
            kind: "ready" as const,
            initialSettings: {
              agentRole: "default",
              supervisor: "off" as const,
              thinking: null,
              fast: null,
              questionsEnabled: true,
              autoCompactionEnabled: false,
            },
          },
        }
      : { ...target, kind: "session" as const, submission: { kind: "ready" as const } }),
    commands,
  });
  return (
    <ChatComposerSurface composer={composer}>
      <ChatComposer availableHeight={null} onHeightChange={() => undefined} editorRef={editorRef} />
    </ChatComposerSurface>
  );
}
