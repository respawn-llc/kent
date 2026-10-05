import { act, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { queryKeys } from "@/app-facade";
import type { ProjectLabel } from "@/api";
import { createTestServices, TestAppProviders } from "@/test-support/app-services";
import { deferred } from "@/test-support/chat-runtime";
import { ProjectLabelsProvider } from "./ProjectLabelsProvider";
import { LabelCatalogRow } from "./LabelCatalogRow";
import type { DeleteState, RenameState } from "./LabelChooserRows";
import { useProjectLabelCatalog } from "./projectLabelHooks";
import { useProjectLabelActions } from "./projectLabelHooks";
import { LabelActionScopeContext } from "./projectLabelContext";

const labelID = "38bf0da7-a3f7-4c15-bc5f-c8fca538e667";

vi.mock("react-i18next", async (importOriginal) => ({
  ...(await importOriginal()),
  useTranslation: () => ({ t: (key: string) => key }),
}));

it("keeps an edited rename draft when a duplicate Save is ignored and the original Save finishes", async () => {
  const services = createTestServices([]);
  const client = new QueryClient();
  const label = { id: "38bf0da7-a3f7-4c15-bc5f-c8fca538e667", name: "Original" };
  client.setQueryData(queryKeys.projectLabels("project-1"), { projectID: "project-1", labels: [label] });
  const first = deferred<ProjectLabel>();
  const second = deferred<ProjectLabel>();
  const rename = vi
    .spyOn(services.api, "renameProjectLabel")
    .mockReturnValueOnce(first.promise)
    .mockReturnValueOnce(second.promise);
  render(
    <TestAppProviders services={services} queryClient={client}>
      <ProjectLabelsProvider projectID="project-1" queryEnabled={false} subscribeToProject={false}>
        <LabelActionScopeContext.Provider value="chooser">
          <RenameEditor />
        </LabelActionScopeContext.Provider>
      </ProjectLabelsProvider>
    </TestAppProviders>,
  );
  const user = userEvent.setup();
  const input = screen.getByRole("textbox", { name: "labels.renameField" });
  await user.clear(input);
  await user.type(input, "First");
  await user.click(screen.getByRole("button", { name: "labels.saveRename" }));
  await user.clear(input);
  await user.type(input, "Second");
  await user.click(screen.getByRole("button", { name: "labels.saveRename" }));
  expect(rename).toHaveBeenCalledTimes(1);
  await act(async () => {
    first.resolve({ ...label, name: "First" });
  });
  expect(screen.getByRole("textbox", { name: "labels.renameField" })).toHaveValue("Second");
  await user.click(screen.getByRole("button", { name: "labels.saveRename" }));
  expect(rename).toHaveBeenLastCalledWith("project-1", label.id, "Second");
  await act(async () => {
    second.resolve({ ...label, name: "Second" });
  });
  expect(screen.queryByRole("textbox", { name: "labels.renameField" })).not.toBeInTheDocument();
});

function RenameEditor() {
  const catalog = useProjectLabelCatalog();
  const label = catalog.data?.labels[0];
  if (label === undefined) throw new Error("Missing catalog fixture");
  const [rename, setRename] = useState<RenameState | null>({ labelID: label.id, draft: label.name });
  const [deletion, setDeletion] = useState<DeleteState | null>(null);
  return (
    <LabelCatalogRow
      label={label}
      rename={rename}
      setRename={setRename}
      deletion={deletion}
      setDeletion={setDeletion}
      highlighted={false}
      reorderPending={false}
      invocation={{ kind: "assignment", selectedLabelIDs: [], onSelectionChange: vi.fn() }}
    />
  );
}

it.each(["rename", "delete"] as const)(
  "keeps %s admission and loading across replacement of its row",
  async (kind) => {
    const services = createTestServices([]);
    const client = new QueryClient();
    client.setQueryData(queryKeys.projectLabels("project-1"), {
      projectID: "project-1",
      labels: [{ id: labelID, name: "Label" }],
    });
    const response = deferred<ProjectLabel>();
    const rename = vi.spyOn(services.api, "renameProjectLabel").mockReturnValue(response.promise);
    const deletion = vi
      .spyOn(services.api, "deleteProjectLabel")
      .mockImplementation(async () => (await response.promise).id);
    const renderRow = (key: string) => (
      <TestAppProviders services={services} queryClient={client}>
        <ProjectLabelsProvider projectID="project-1" queryEnabled={false} subscribeToProject={false}>
          <LabelActionScopeContext.Provider value="chooser">
            <ActionRow key={key} kind={kind} />
          </LabelActionScopeContext.Provider>
        </ProjectLabelsProvider>
      </TestAppProviders>
    );
    const view = render(renderRow("original"));
    const user = userEvent.setup();
    await user.click(screen.getByRole("button", { name: kind }));
    expect(screen.getByRole("button", { name: kind })).toHaveAttribute("aria-busy", "true");
    view.rerender(renderRow("replacement"));
    expect(screen.getByRole("button", { name: kind })).toHaveAttribute("aria-busy", "true");
    await user.click(screen.getByRole("button", { name: kind }));
    expect(kind === "rename" ? rename : deletion).toHaveBeenCalledTimes(1);
    await act(async () => {
      response.resolve({ id: labelID, name: "Saved" });
    });
  },
);

function ActionRow({ kind }: Readonly<{ kind: "rename" | "delete" }>) {
  const actions = useProjectLabelActions(labelID);
  const action = actions[kind];
  return (
    <button
      aria-busy={action.isPending}
      onClick={() => {
        if (kind === "rename") actions.rename.submit({ name: "Saved" });
        else actions.delete.submit({});
      }}
    >
      {kind}
    </button>
  );
}
