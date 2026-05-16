export type ServerCause = Readonly<{
  code: string;
  severity: string;
  summary: string;
  nextAction: string;
  diagnosticID: string;
}>;

export type ServerReadiness = Readonly<{
  ready: boolean;
  serverID: string;
  serverVersion: string;
  protocolVersion: string;
  authReady: boolean;
  authRequired: boolean;
  endpoint: string;
  causes: readonly ServerCause[];
}>;

export type ServerCapability = Readonly<{
  id: string;
  available: boolean;
  reason: string;
  requiredForMvp: boolean;
}>;

export type ServerCapabilities = Readonly<{
  capabilities: readonly ServerCapability[];
  serverVersion: string;
  protocolVersion: string;
}>;

export type WorkspaceSummary = Readonly<{
  id: string;
  name: string;
  rootPath: string;
  availability: string;
  isPrimary: boolean;
  updatedAt: number;
}>;

export type ProjectSummary = Readonly<{
  id: string;
  key: string;
  name: string;
  primaryWorkspace: WorkspaceSummary;
  defaultWorkflowID: string;
  defaultWorkflowName: string;
  defaultWorkflowValid: boolean;
  updatedAt: number;
  taskCount: number;
  attentionCount: number;
  workflowCount: number;
}>;

export type ProjectPage = Readonly<{
  projects: readonly ProjectSummary[];
  nextPageToken: string;
  generatedAt: number;
  latestEventSequence: number;
}>;

export type WorkspaceList = Readonly<{
  projectID: string;
  workspaces: readonly WorkspaceSummary[];
  defaultWorkspaceID: string;
}>;

export type ProjectBinding = Readonly<{
  projectID: string;
  projectKey: string;
  projectName: string;
  workspaceID: string;
  canonicalRoot: string;
  workspaceName: string;
  workspaceStatus: string;
}>;

export type BindingPlan = Readonly<{
  kind: string;
  canonicalRoot: string;
  binding: ProjectBinding | null;
}>;

export type PendingAsk = Readonly<{
  askID: string;
  sessionID: string;
  question: string;
  suggestions: readonly string[];
  recommendedOptionIndex: number;
  createdAt: string;
}>;

export type WorkflowValidationError = Readonly<{
  code: string;
  message: string;
  nodeID: string;
  edgeID: string;
  blocksContext: boolean;
}>;

export type WorkflowPickerItem = Readonly<{
  id: string;
  name: string;
  description: string;
  graphRevision: number;
  isProjectDefault: boolean;
  validForTaskCreation: boolean;
  validationErrors: readonly WorkflowValidationError[];
}>;

export type TaskStatus = Readonly<{
  kind: string;
  label: string;
  nativeState: string;
  nodeIDs: readonly string[];
  runIDs: readonly string[];
  attentionTypes: readonly string[];
}>;

export type TaskActions = Readonly<{
  canStart: boolean;
  canInterrupt: boolean;
  interruptRunID: string;
  canResume: boolean;
  resumeRunID: string;
  canCancel: boolean;
  needsDetailForInterrupt: boolean;
  needsDetailForResume: boolean;
}>;

export type BoardCard = Readonly<{
  id: string;
  shortID: string;
  title: string;
  bodyPreview: string;
  workflowID: string;
  activeNodeIDs: readonly string[];
  sourceWorkspace: WorkspaceSummary;
  status: TaskStatus;
  actions: TaskActions;
  updatedAt: number;
}>;

export type BoardColumn = Readonly<{
  id: string;
  key: string;
  name: string;
  groupID: string;
  sortOrder: number;
  isBacklog: boolean;
  isDone: boolean;
  taskCount: number;
}>;

export type BoardGroup = Readonly<{
  id: string;
  key: string;
  name: string;
  sortOrder: number;
  nodeIDs: readonly string[];
}>;

export type WorkflowBoard = Readonly<{
  projectID: string;
  projectKey: string;
  projectName: string;
  selectedWorkflow: WorkflowPickerItem;
  workflows: readonly WorkflowPickerItem[];
  groups: readonly BoardGroup[];
  columns: readonly BoardColumn[];
  cards: readonly BoardCard[];
  donePreview: readonly BoardCard[];
  nextPageToken: string;
  generatedAt: number;
  latestEventSequence: number;
}>;

export type AttentionItem = Readonly<{
  id: string;
  kind: string;
  projectID: string;
  workflowID: string;
  taskID: string;
  taskShortID: string;
  taskTitle: string;
  runID: string;
  sessionID: string;
  askID: string;
  taskTransitionID: string;
  message: string;
  occurredAt: number;
  latestEventSequence: number;
}>;

export type AttentionPage = Readonly<{
  items: readonly AttentionItem[];
  nextPageToken: string;
  generatedAt: number;
  latestEventSequence: number;
}>;

export type TaskComment = Readonly<{
  id: string;
  taskID: string;
  body: string;
  author: string;
  deletedAt: number;
  createdAt: number;
  updatedAt: number;
}>;

export type TaskRun = Readonly<{
  id: string;
  taskID: string;
  placementID: string;
  nodeID: string;
  sessionID: string;
  sessionName: string;
  role: string;
  status: string;
  generation: number;
  waitingAskID: string;
  startedAt: number;
  completedAt: number;
  interruptedAt: number;
}>;

export type TaskTransition = Readonly<{
  id: string;
  transitionID: string;
  transitionName: string;
  sourceNodeName: string;
  state: string;
  commentary: string;
  outputValues: Readonly<Record<string, string>>;
  edges: readonly TransitionEdge[];
  graphRevision: number;
  createdAt: number;
  appliedAt: number;
}>;

export type TransitionEdge = Readonly<{
  id: string;
  edgeKey: string;
  targetNodeName: string;
  state: string;
  requiresApproval: boolean;
  outputRequirements: readonly string[];
}>;

export type TaskDetail = Readonly<{
  id: string;
  shortID: string;
  projectID: string;
  projectName: string;
  workflowID: string;
  workflowName: string;
  title: string;
  body: string;
  sourceWorkspace: WorkspaceSummary;
  status: TaskStatus;
  actions: TaskActions;
  attention: readonly AttentionItem[];
  comments: readonly TaskComment[];
  runs: readonly TaskRun[];
  transitions: readonly TaskTransition[];
  worktreePath: string;
  createdAt: number;
  updatedAt: number;
  done: boolean;
  canceledAt: number;
}>;

export type ActivityItem = Readonly<{
  id: string;
  type: string;
  taskID: string;
  occurredAt: number;
  updatedAt: number;
  actor: string;
  summary: string;
  comment: TaskComment | null;
  transition: TaskTransition | null;
  run: TaskRun | null;
  attention: AttentionItem | null;
}>;

export type ActivityPage = Readonly<{
  items: readonly ActivityItem[];
  nextPageToken: string;
  generatedAt: number;
}>;

export type TeleportTarget = Readonly<{
  available: boolean;
  taskID: string;
  runID: string;
  sessionID: string;
  projectID: string;
  workspaceID: string;
  worktreeID: string;
  cwdRelpath: string;
  failureReason: string;
}>;
