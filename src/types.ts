export type PermissionMode = "approve-all" | "approve-reads" | "deny-all";
export type SessionStatus = "pending" | "running" | "complete" | "error";
export type SessionStatusFilter = SessionStatus | "active";
export type SessionEventType = "status" | "output" | "error" | "done";
export type TeleCoderPolicyMode = "locked" | "observe" | "standard";
export type WorkspaceWritePolicy = "blocked" | "contained";
export type FailureKind = "" | "abandoned" | "policy_denied" | "runtime_error" | "timeout";
export type WatchKind = "ci_failure" | "pr_review";
export type WatchStatus = "active" | "paused";

export interface TeleCoderConfig {
  dataDir: string;
  dbPath: string;
  workspaceDir: string;
  listenHost: string;
  listenPort: number;
  acpxCommand: string;
  agentCommands: Record<string, string>;
  defaultAgent: string;
  defaultPolicyMode: TeleCoderPolicyMode;
  permissionMode: PermissionMode;
  workspaceWritePolicy: WorkspaceWritePolicy;
  policyMaxRuntimeSeconds: number;
  promptTimeoutSeconds: number;
  sessionHeartbeatSeconds: number;
  sessionStaleSeconds: number;
}

export interface SessionRecord {
  id: string;
  repo: string;
  prompt: string;
  agent: string;
  parentSessionId: string;
  attempt: number;
  policyMode: TeleCoderPolicyMode;
  effectivePermissionMode: PermissionMode;
  workspaceWritePolicy: WorkspaceWritePolicy;
  maxRuntimeSeconds: number;
  runtimeCommand: string;
  status: SessionStatus;
  ownerId: string;
  branch: string;
  workDir: string;
  resultText: string;
  outcomeHeadline: string;
  outcomeChanged: string;
  outcomeVerified: string;
  outcomeUncertain: string;
  outcomeNext: string;
  error: string;
  failureKind: FailureKind;
  claimedAt: string;
  heartbeatAt: string;
  startedAt: string;
  finishedAt: string;
  createdAt: string;
  updatedAt: string;
}

export interface SessionEvent {
  id: number;
  sessionId: string;
  type: SessionEventType;
  data: string;
  createdAt: string;
}

export interface SessionListQuery {
  agent?: string;
  lineageSessionId?: string;
  parentSessionId?: string;
  policyMode?: TeleCoderPolicyMode;
  status?: SessionStatusFilter;
}

export interface CreateTaskInput {
  repo: string;
  prompt: string;
  agent?: string;
  attempt?: number;
  parentSessionId?: string;
  policyMode?: TeleCoderPolicyMode;
}

export interface TaskRunInput {
  acpxCommand: string;
  agent: string;
  agentCommand?: string;
  cwd: string;
  prompt: string;
  permissionMode: PermissionMode;
  timeoutSeconds: number;
}

export interface TaskRunResult {
  output: string;
}

export interface WorkspaceInfo {
  branch: string;
  workDir: string;
}

export interface WatchRecord {
  id: string;
  kind: WatchKind;
  repo: string;
  instructions: string;
  agent: string;
  policyMode: TeleCoderPolicyMode;
  workflowName: string;
  branchName: string;
  baseBranch: string;
  headBranch: string;
  status: WatchStatus;
  createdAt: string;
  updatedAt: string;
}

export interface WatchRunRecord {
  id: number;
  watchId: string;
  sessionId: string;
  eventKey: string;
  sourceRef: string;
  triggerSummary: string;
  resultSummary: string;
  returnSummary: string;
  createdAt: string;
}

export interface WatchListQuery {
  kind?: WatchKind;
  repo?: string;
  status?: WatchStatus;
}

export interface CreateCiWatchInput {
  repo: string;
  instructions: string;
  agent?: string;
  policyMode?: TeleCoderPolicyMode;
  workflowName?: string;
  branchName?: string;
}

export interface CreatePrWatchInput {
  repo: string;
  instructions: string;
  agent?: string;
  policyMode?: TeleCoderPolicyMode;
  baseBranch?: string;
  headBranch?: string;
}

export interface CiWatchEvent {
  repo: string;
  workflowName: string;
  branchName: string;
  runId: string;
  runUrl?: string;
  sha?: string;
  status?: string;
  conclusion?: string;
  summary?: string;
}

export interface PrWatchEvent {
  repo: string;
  prNumber: number;
  title: string;
  baseBranch: string;
  headBranch: string;
  action?: string;
  prUrl?: string;
  headSha?: string;
  body?: string;
  diffText?: string;
}

export interface PublishSessionInput {
  baseBranch: string;
  title?: string;
  body?: string;
}

export interface PublishSessionResult {
  sessionId: string;
  branch: string;
  title: string;
  pullRequestNumber: number;
  pullRequestUrl: string;
}
