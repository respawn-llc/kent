import { useState, type SyntheticEvent } from "react";
import { useTranslation } from "react-i18next";

import type { AttentionItem, ProjectSummary } from "../../api";
import { errorMessage } from "../../api/errors";
import { basename, formatRelativeTime, projectKeyFromName } from "../../app/formatters";
import { useAppNavigation } from "../../app/navigation";
import { useAppServices } from "../../app/useAppServices";
import { useStatusController } from "../../app/useStatusController";
import { useConnectionSnapshot } from "../../app/useConnectionSnapshot";
import { Badge, Button, EmptyState, ErrorState, Island, TextInput } from "../../ui";
import { useGlobalAttentionPages, useProjectCreation, useProjectPages, useWorkspaceAttach } from "./useHomeData";

type ProjectDraft = Readonly<{
  name: string;
  key: string;
  workspaceRoot: string;
}>;

export function HomeRoute() {
  const { t } = useTranslation();
  const projects = useProjectPages();
  const attention = useGlobalAttentionPages();
  const projectItems = projects.data?.pages.flatMap((page) => page.projects) ?? [];
  const attentionItems = attention.data?.pages.flatMap((page) => page.items) ?? [];

  return (
    <div className="home-page">
      <HomeHeader />
      <div className="home-grid">
        <Island aria-labelledby="projects-title" tone="secondary">
          <h2 id="projects-title">{t("home.projectsPane")}</h2>
          <ProjectList items={projectItems} query={projects} />
        </Island>
        <Island aria-labelledby="attention-title" tone="secondary">
          <h2 id="attention-title">{t("home.attentionPane")}</h2>
          <AttentionList items={attentionItems} query={attention} />
        </Island>
      </div>
    </div>
  );
}

function HomeHeader() {
  const { t } = useTranslation();
  const { api, endpoint, nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const connection = useConnectionSnapshot();
  const navigation = useAppNavigation();
  const creation = useProjectCreation();
  const [draft, setDraft] = useState<ProjectDraft | null>(null);
  const disabled = connection.phase !== "connected";

  async function chooseWorkspace(): Promise<void> {
    try {
      const selected = await nativeBridge.directories.selectDirectory({ title: t("home.chooseWorkspace") });
      if (selected === null) {
        return;
      }
      const plan = await api.planWorkspace(selected.path);
      if (plan.binding !== null) {
        navigation.openProject(plan.binding.projectID);
        return;
      }
      const name = basename(plan.canonicalRoot);
      setDraft({ name, key: projectKeyFromName(name), workspaceRoot: plan.canonicalRoot });
    } catch (error) {
      push({ id: "project-create-error", tone: "danger", title: t("form.serverError"), body: errorMessage(error) });
    }
  }

  async function submitDraft(event: SyntheticEvent<HTMLFormElement>): Promise<void> {
    event.preventDefault();
    if (draft === null) {
      return;
    }
    const binding = await creation.mutateAsync(draft);
    setDraft(null);
    navigation.openProject(binding.projectID);
  }

  return (
    <header className="page-header">
      <div>
        <p className="eyebrow">{t("app.endpoint")}</p>
        <h1>{t("home.title")}</h1>
        <p className="mono">{endpoint}</p>
      </div>
      <div className="page-actions">
        <Button disabled={disabled} onClick={() => void chooseWorkspace()} variant="primary">
          {t("home.newProject")}
        </Button>
      </div>
      {draft !== null ? (
        <form className="inline-form" onSubmit={(event) => void submitDraft(event)}>
          <TextInput label={t("home.projectName")} onChange={(event) => { setDraft({ ...draft, name: event.target.value }); }} value={draft.name} />
          <TextInput label={t("home.projectKey")} onChange={(event) => { setDraft({ ...draft, key: event.target.value }); }} value={draft.key} />
          <TextInput disabled label={t("home.workspaceRoot")} value={draft.workspaceRoot} />
          {creation.error !== null ? <p className="form-error">{errorMessage(creation.error)}</p> : null}
          <Button disabled={creation.isPending || disabled} type="submit" variant="primary">
            {t("home.createProject")}
          </Button>
        </form>
      ) : null}
    </header>
  );
}

type ProjectListProps = Readonly<{
  items: readonly ProjectSummary[];
  query: ReturnType<typeof useProjectPages>;
}>;

function ProjectList({ items, query }: ProjectListProps) {
  const { t } = useTranslation();
  if (query.isPending) {
    return <p>{t("states.loading")}</p>;
  }
  if (query.isError) {
    return <ErrorState body={errorMessage(query.error)} title={t("states.error")} />;
  }
  if (items.length === 0) {
    return <EmptyState body={t("home.emptyBody")} title={t("home.emptyTitle")} />;
  }
  return (
    <div className="row-list">
      {items.map((project) => (
        <ProjectRow key={project.id} project={project} />
      ))}
      {query.hasNextPage ? (
        <Button disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()} variant="ghost">
          {query.isFetchingNextPage ? t("app.loadingMore") : t("app.loadMore")}
        </Button>
      ) : null}
    </div>
  );
}

function ProjectRow({ project }: Readonly<{ project: ProjectSummary }>) {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  const { nativeBridge } = useAppServices();
  const { push } = useStatusController();
  const attach = useWorkspaceAttach();
  const connection = useConnectionSnapshot();

  async function addWorkspace(): Promise<void> {
    try {
      const selected = await nativeBridge.directories.selectDirectory({ title: t("home.chooseWorkspace") });
      if (selected !== null) {
        await attach.mutateAsync({ projectID: project.id, workspaceRoot: selected.path });
      }
    } catch (error) {
      push({ id: `attach-${project.id}`, tone: "danger", title: t("form.serverError"), body: errorMessage(error) });
    }
  }

  return (
    <article className="project-row">
      <button className="project-row__main" onClick={() => { navigation.openProject(project.id, project.defaultWorkflowID); }} type="button">
        <span className="mono">{project.key}</span>
        <strong>{project.name}</strong>
        <span>{project.primaryWorkspace.rootPath}</span>
        <span>{t("home.updated", { time: formatRelativeTime(project.updatedAt) })}</span>
      </button>
      <div className="chip-row">
        <Badge tone={project.defaultWorkflowValid ? "success" : "warning"}>{project.defaultWorkflowName || t("board.noWorkflowTitle")}</Badge>
        <Badge tone="info">
          {t("home.projectChips", {
            tasks: project.taskCount,
            attention: project.attentionCount,
            workflows: project.workflowCount,
          })}
        </Badge>
        <Button disabled={connection.phase !== "connected" || attach.isPending} onClick={() => void addWorkspace()} variant="ghost">
          {t("home.addWorkspace")}
        </Button>
      </div>
    </article>
  );
}

type AttentionListProps = Readonly<{
  items: readonly AttentionItem[];
  query: ReturnType<typeof useGlobalAttentionPages>;
}>;

function AttentionList({ items, query }: AttentionListProps) {
  const { t } = useTranslation();
  const navigation = useAppNavigation();
  if (query.isPending) {
    return <p>{t("states.loading")}</p>;
  }
  if (query.isError) {
    return <ErrorState body={errorMessage(query.error)} title={t("states.error")} />;
  }
  if (items.length === 0) {
    return <EmptyState body={t("home.noAttentionBody")} title={t("home.noAttentionTitle")} />;
  }
  return (
    <div className="row-list">
      {items.map((item) => (
        <button className="attention-row" key={item.id} onClick={() => { navigation.openTask(item.taskID); }} type="button">
          <Badge tone="warning">{item.kind}</Badge>
          <strong>{item.taskTitle}</strong>
          <span className="mono">{item.taskShortID}</span>
          <span>{item.message}</span>
          <span>{formatRelativeTime(item.occurredAt)}</span>
        </button>
      ))}
      {query.hasNextPage ? (
        <Button disabled={query.isFetchingNextPage} onClick={() => void query.fetchNextPage()} variant="ghost">
          {query.isFetchingNextPage ? t("app.loadingMore") : t("app.loadMore")}
        </Button>
      ) : null}
    </div>
  );
}
