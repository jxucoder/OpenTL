import { randomUUID } from "node:crypto";

import { publishSession } from "./publish.ts";
import { resolvePolicy } from "./policy.ts";
import { TeleCoderStore } from "./store.ts";
import { prepareWorkspace } from "./workspace.ts";
import { AcpxRuntime, TaskRuntimeError, type TaskRuntime } from "./runtime/acpx.ts";
import {
  buildCiEventKey,
  buildCiTriggerSummary,
  buildCiWatchPrompt,
  buildPrEventKey,
  buildPrTriggerSummary,
  buildPrWatchPrompt,
  defaultCiWatchPolicy,
  defaultPrWatchPolicy,
  isCiFailureEvent,
  isPrReviewEvent,
  loadPrDiffContext,
  matchesCiWatch,
  matchesPrWatch,
} from "./watch.ts";
import type {
  CiWatchEvent,
  CreateCiWatchInput,
  CreatePrWatchInput,
  CreateTaskInput,
  FailureKind,
  PublishSessionInput,
  PublishSessionResult,
  PrWatchEvent,
  SessionEvent,
  SessionListQuery,
  SessionRecord,
  TeleCoderConfig,
  WatchListQuery,
  WatchRecord,
  WatchRunRecord,
} from "./types.ts";

type Listener = (event: SessionEvent) => void;

export class TeleCoderEngine {
  private readonly listeners = new Map<string, Set<Listener>>();
  private readonly ownerId = randomUUID();
  private readonly runtime: TaskRuntime;
  private readonly store: TeleCoderStore;

  constructor(
    private readonly config: TeleCoderConfig,
    store?: TeleCoderStore,
    runtime?: TaskRuntime,
  ) {
    this.store = store ?? new TeleCoderStore(config.dbPath);
    this.runtime = runtime ?? new AcpxRuntime();
    this.recoverPendingSessions();
    this.recoverStaleRunningSessions();
  }

  close(): void {
    this.store.close();
  }

  async createTask(input: CreateTaskInput): Promise<SessionRecord> {
    const session = this.enqueueTask(input);

    queueMicrotask(() => {
      void this.runTask(session.id);
    });

    return session;
  }

  async rerunSession(sessionId: string): Promise<SessionRecord> {
    const source = this.store.getSession(sessionId);
    if (!source) {
      throw new Error(`Session ${sessionId} not found`);
    }

    if (source.status === "pending" || source.status === "running") {
      throw new Error(`Session ${sessionId} is still active and cannot be rerun`);
    }

    const rerun = this.enqueueTask({
      repo: source.repo,
      prompt: source.prompt,
      agent: source.agent,
      parentSessionId: source.id,
      attempt: source.attempt + 1,
      policyMode: source.policyMode,
    });
    this.emit(rerun.id, "status", `Rerun requested from session ${source.id}`);

    queueMicrotask(() => {
      void this.runTask(rerun.id);
    });

    return rerun;
  }

  private enqueueTask(input: CreateTaskInput): SessionRecord {
    const claimedAt = new Date().toISOString();
    const agent = input.agent ?? this.config.defaultAgent;
    const policy = resolvePolicy(this.config, input.policyMode);

    return this.store.createSession({
      id: randomUUID().slice(0, 8),
      repo: input.repo,
      prompt: input.prompt,
      agent,
      parentSessionId: input.parentSessionId,
      attempt: input.attempt,
      policyMode: policy.policyMode,
      effectivePermissionMode: policy.effectivePermissionMode,
      workspaceWritePolicy: policy.workspaceWritePolicy,
      maxRuntimeSeconds: policy.maxRuntimeSeconds,
      runtimeCommand: this.config.agentCommands[agent] ?? agent,
      ownerId: this.ownerId,
      claimedAt,
      heartbeatAt: claimedAt,
    });
  }

  getSession(id: string): SessionRecord | null {
    return this.store.getSession(id);
  }

  async publishSession(
    sessionId: string,
    input: PublishSessionInput,
  ): Promise<PublishSessionResult> {
    const session = this.store.getSession(sessionId);
    if (!session) {
      throw new Error(`Session ${sessionId} not found`);
    }

    return await publishSession(session, input);
  }

  listSessions(query?: SessionListQuery): SessionRecord[] {
    return this.store.listSessions(query);
  }

  listInbox(limit = 20): SessionRecord[] {
    return this.store.listInboxSessions(limit);
  }

  createCiWatch(input: CreateCiWatchInput): WatchRecord {
    return this.store.createWatch({
      id: randomUUID().slice(0, 8),
      kind: "ci_failure",
      repo: input.repo,
      instructions: input.instructions,
      agent: input.agent,
      policyMode: input.policyMode ?? defaultCiWatchPolicy(),
      workflowName: input.workflowName,
      branchName: input.branchName,
    });
  }

  createPrWatch(input: CreatePrWatchInput): WatchRecord {
    return this.store.createWatch({
      id: randomUUID().slice(0, 8),
      kind: "pr_review",
      repo: input.repo,
      instructions: input.instructions,
      agent: input.agent,
      policyMode: input.policyMode ?? defaultPrWatchPolicy(),
      baseBranch: input.baseBranch,
      headBranch: input.headBranch,
    });
  }

  getWatch(id: string): WatchRecord | null {
    return this.store.getWatch(id);
  }

  listWatches(query?: WatchListQuery): WatchRecord[] {
    return this.store.listWatches(query);
  }

  listWatchRuns(watchId: string): WatchRunRecord[] {
    return this.store.listWatchRuns(watchId);
  }

  async triggerCiWatchEvent(
    event: CiWatchEvent,
  ): Promise<Array<{ run: WatchRunRecord; session: SessionRecord; watch: WatchRecord }>> {
    if (!isCiFailureEvent(event)) {
      return [];
    }

    const triggered: Array<{ run: WatchRunRecord; session: SessionRecord; watch: WatchRecord }> = [];
    const watches = this.store.listWatches({ kind: "ci_failure", status: "active", repo: event.repo });

    for (const watch of watches) {
      if (!matchesCiWatch(watch, event)) {
        continue;
      }

      const eventKey = buildCiEventKey(event);
      const reservation = this.store.reserveWatchRun({
        watchId: watch.id,
        eventKey,
        triggerSummary: buildCiTriggerSummary(watch, event),
        sourceRef: event.runUrl,
      });
      if (!reservation) {
        continue;
      }

      try {
        const session = await this.createTask({
          repo: watch.repo,
          prompt: buildCiWatchPrompt(watch, event),
          agent: watch.agent || undefined,
          policyMode: watch.policyMode,
        });
        const run = this.store.attachWatchRunSession(reservation.id, session.id);

        triggered.push({ watch, run, session });
      } catch (error) {
        this.store.deleteWatchRun(reservation.id);
        throw error;
      }
    }

    return triggered;
  }

  async triggerPrWatchEvent(
    event: PrWatchEvent,
  ): Promise<Array<{ run: WatchRunRecord; session: SessionRecord; watch: WatchRecord }>> {
    if (!isPrReviewEvent(event)) {
      return [];
    }

    const triggered: Array<{ run: WatchRunRecord; session: SessionRecord; watch: WatchRecord }> =
      [];
    const watches = this.store.listWatches({ kind: "pr_review", status: "active", repo: event.repo });
    const diffContext = await loadPrDiffContext(event);

    for (const watch of watches) {
      if (!matchesPrWatch(watch, event)) {
        continue;
      }

      const eventKey = buildPrEventKey(event);
      const reservation = this.store.reserveWatchRun({
        watchId: watch.id,
        eventKey,
        triggerSummary: buildPrTriggerSummary(watch, event),
        sourceRef: event.prUrl,
      });
      if (!reservation) {
        continue;
      }

      try {
        const session = await this.createTask({
          repo: watch.repo,
          prompt: buildPrWatchPrompt(watch, event, diffContext),
          agent: watch.agent || undefined,
          policyMode: watch.policyMode,
        });
        const run = this.store.attachWatchRunSession(reservation.id, session.id);

        triggered.push({ watch, run, session });
      } catch (error) {
        this.store.deleteWatchRun(reservation.id);
        throw error;
      }
    }

    return triggered;
  }

  listEvents(sessionId: string, afterId = 0): SessionEvent[] {
    return this.store.listEvents(sessionId, afterId);
  }

  subscribe(sessionId: string, listener: Listener): () => void {
    const current = this.listeners.get(sessionId) ?? new Set<Listener>();
    current.add(listener);
    this.listeners.set(sessionId, current);

    return () => {
      const existing = this.listeners.get(sessionId);
      if (!existing) {
        return;
      }
      existing.delete(listener);
      if (existing.size === 0) {
        this.listeners.delete(sessionId);
      }
    };
  }

  private emit(sessionId: string, type: SessionEvent["type"], data: string): SessionEvent {
    const event = this.store.insertEvent({ sessionId, type, data });
    for (const listener of this.listeners.get(sessionId) ?? []) {
      listener(event);
    }
    return event;
  }

  private async runTask(sessionId: string): Promise<void> {
    const startedAt = new Date().toISOString();
    let session = this.store.claimPendingSession(sessionId, this.ownerId, startedAt);
    if (!session) {
      return;
    }

    const stopHeartbeat = this.startHeartbeat(sessionId);
    try {
      this.emit(sessionId, "status", this.describePolicy(session));
      this.emit(sessionId, "status", "setting up workspace");
      const workspace = await prepareWorkspace(this.config.workspaceDir, sessionId, session.repo);
      session = this.store.updateSession(sessionId, {
        branch: workspace.branch,
        workDir: workspace.workDir,
      });

      this.emit(sessionId, "status", "running acpx");
      const result = await this.runtime.runTask({
        acpxCommand: this.config.acpxCommand,
        agent: session.agent,
        agentCommand: session.runtimeCommand === session.agent ? undefined : session.runtimeCommand,
        cwd: session.workDir,
        prompt: session.prompt,
        permissionMode: session.effectivePermissionMode,
        timeoutSeconds: session.maxRuntimeSeconds,
      });

      if (result.output) {
        this.emit(sessionId, "output", result.output);
      }

      this.store.updateSession(sessionId, {
        ownerId: this.ownerId,
        resultText: result.output,
        status: "complete",
        error: "",
        failureKind: "",
        finishedAt: new Date().toISOString(),
      });
      this.emit(sessionId, "done", "text");
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      const failureKind = this.classifyFailure(error);
      this.store.updateSession(sessionId, {
        ownerId: this.ownerId,
        error: message,
        failureKind,
        status: "error",
        finishedAt: new Date().toISOString(),
      });
      this.emit(sessionId, "error", message);
    } finally {
      stopHeartbeat();
    }
  }

  private recoverPendingSessions(): void {
    const recovered = this.store.recoverPendingSessions(
      this.ownerId,
      "Recovered pending session after restart; requeued.",
    );

    for (const session of recovered) {
      queueMicrotask(() => {
        void this.runTask(session.id);
      });
    }
  }

  private recoverStaleRunningSessions(): void {
    const cutoff = new Date(Date.now() - this.config.sessionStaleSeconds * 1000).toISOString();
    this.store.reconcileStaleRunningSessions(
      cutoff,
      `Session abandoned after ${this.config.sessionStaleSeconds}s without heartbeat; rerun required.`,
    );
  }

  private startHeartbeat(sessionId: string): () => void {
    const intervalMs = this.config.sessionHeartbeatSeconds * 1000;
    const timer = setInterval(() => {
      try {
        this.store.updateSession(sessionId, {
          ownerId: this.ownerId,
          heartbeatAt: new Date().toISOString(),
        });
      } catch {
        clearInterval(timer);
      }
    }, intervalMs);

    return () => {
      clearInterval(timer);
    };
  }

  private classifyFailure(error: unknown): FailureKind {
    if (error instanceof TaskRuntimeError) {
      return error.failureKind;
    }

    return "runtime_error";
  }

  private describePolicy(session: SessionRecord): string {
    return `policy ${session.policyMode}: permissions=${session.effectivePermissionMode}, workspace-writes=${session.workspaceWritePolicy}, max-runtime=${session.maxRuntimeSeconds}s`;
  }
}
