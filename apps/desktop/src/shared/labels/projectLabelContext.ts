import { createContext, useContext } from "react";
import type { ProjectLabelsModel } from "./ProjectLabelsModel";

export type ProjectLabelDataContextValue = ProjectLabelsModel;

export const ProjectLabelDataContext = createContext<ProjectLabelDataContextValue | null>(null);
export const LabelActionScopeContext = createContext<string | null>(null);

export function useProjectLabelData(): ProjectLabelDataContextValue {
  const value = useContext(ProjectLabelDataContext);
  if (value === null) {
    throw new Error("ProjectLabelsProvider is required");
  }
  return value;
}
