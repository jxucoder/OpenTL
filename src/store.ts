import { Database } from "bun:sqlite";

import { deriveSessionOutcome } from "./outcome.ts";
import { buildWatchReturnSummary, summarizeWatchOutcome } from "./watch.ts";
import type {
  SessionEvent,
  SessionEventType,
  SessionListQuery,
  SessionRecord,
  WatchListQuery,
  WatchRecord,
  WatchRunRecord,
} from "./types.ts";

interface SessionRow {
  agent: string;
  attempt: number;
  branch: string;
  claimed_at: string;
  created_at: string;
  error: string;
  failure_kind: SessionRecord["failureKind"];
  effective_permission_mode: SessionRecord["effectivePermissionMode"];
  finished_at: string;
  heartbeat_at: string;
  id: string;
  max_runtime_seconds: number;
  owner_id: string;
  parent_session_id: string;
  policy_mode: SessionRecord["policyMode"];
  prompt: string;
  repo: string;
  result_text: string;
  outcome_changed: string;
  outcome_headline: string;
  outcome_next: string;
  outcome_uncertain: string;
  outcome_verified: string;
  runtime_command: string;
  started_at: string;
  status: SessionRecord["status"];
  updated_at: string;
  work_dir: string;
  workspace_write_policy: SessionRecord["workspaceWritePolicy"];
}

interface EventRow {
  created_at: string;
  data: string;
  id: number;
  session_id: string;
  type: SessionEventType;
}

interface WatchRow {
  agent: string;
  base_branch: string;
  branch_name: string;
  created_at: string;
  head_branch: string;
  id: string;
  instructions: string;
  kind: WatchRecord["kind"];
  policy_mode: WatchRecord["policyMode"];
  repo: string;
  status: WatchRecord["status"];
  updated_at: string;
  workflow_name: string;
}

interface WatchRunRow {
  created_at: string;
  event_key: string;
  id: number;
  result_agent: string;
  result_error: string;
  result_outcome_changed: string;
  result_outcome_headline: string;
  result_outcome_next: string;
  result_outcome_uncertain: string;
  result_outcome_verified: string;
  result_policy_mode: SessionRecord["policyMode"];
  result_status: SessionRecord["status"] | null;
  result_text: string;
  result_runtime_command: string;
  result_workspace_write_policy: SessionRecord["workspaceWritePolicy"];
  session_id: string;
  source_ref: string;
  trigger_summary: string;
  watch_id: string;
}

function nowIso(): string {
  return new Date().toISOString();
}

function isUniqueConstraintError(error: unknown, tableColumns: string): boolean {
  if (!(error instanceof Error)) {
    return false;
  }

  return (
    error.message.includes("UNIQUE constraint failed") && error.message.includes(tableColumns)
  );
}

function mapSession(row: SessionRow): SessionRecord {
  const derivedOutcome =
    row.outcome_headline ||
    row.outcome_changed ||
    row.outcome_verified ||
    row.outcome_uncertain ||
    row.outcome_next
      ? null
      : deriveSessionOutcome({
          status: row.status,
          resultText: row.result_text,
          error: row.error,
        });

  return {
    id: row.id,
    repo: row.repo,
    prompt: row.prompt,
    agent: row.agent,
    parentSessionId: row.parent_session_id,
    attempt: row.attempt,
    policyMode: row.policy_mode,
    effectivePermissionMode: row.effective_permission_mode,
    workspaceWritePolicy: row.workspace_write_policy,
    maxRuntimeSeconds: row.max_runtime_seconds,
    runtimeCommand: row.runtime_command,
    status: row.status,
    ownerId: row.owner_id,
    branch: row.branch,
    workDir: row.work_dir,
    resultText: row.result_text,
    outcomeHeadline: row.outcome_headline || derivedOutcome?.outcomeHeadline || "",
    outcomeChanged: row.outcome_changed || derivedOutcome?.outcomeChanged || "",
    outcomeVerified: row.outcome_verified || derivedOutcome?.outcomeVerified || "",
    outcomeUncertain: row.outcome_uncertain || derivedOutcome?.outcomeUncertain || "",
    outcomeNext: row.outcome_next || derivedOutcome?.outcomeNext || "",
    error: row.error,
    failureKind: row.failure_kind,
    claimedAt: row.claimed_at,
    heartbeatAt: row.heartbeat_at,
    startedAt: row.started_at,
    finishedAt: row.finished_at,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function mapWatch(row: WatchRow): WatchRecord {
  return {
    id: row.id,
    kind: row.kind,
    repo: row.repo,
    instructions: row.instructions,
    agent: row.agent,
    policyMode: row.policy_mode,
    workflowName: row.workflow_name,
    branchName: row.branch_name,
    baseBranch: row.base_branch,
    headBranch: row.head_branch,
    status: row.status,
    createdAt: row.created_at,
    updatedAt: row.updated_at,
  };
}

function mapWatchRun(row: WatchRunRow): WatchRunRecord {
  const resultSummary = row.result_status
    ? summarizeWatchOutcome({
        outcomeChanged: row.result_outcome_changed,
        outcomeHeadline: row.result_outcome_headline,
        outcomeNext: row.result_outcome_next,
        outcomeUncertain: row.result_outcome_uncertain,
        outcomeVerified: row.result_outcome_verified,
        status: row.result_status,
        resultText: row.result_text,
        error: row.result_error,
      })
    : "";

  return {
    id: row.id,
    watchId: row.watch_id,
    sessionId: row.session_id,
    eventKey: row.event_key,
    sourceRef: row.source_ref,
    triggerSummary: row.trigger_summary,
    resultSummary,
    returnSummary: row.result_status
      ? buildWatchReturnSummary(row.trigger_summary, {
          agent: row.result_agent,
          error: row.result_error,
          outcomeChanged: row.result_outcome_changed,
          outcomeHeadline: row.result_outcome_headline,
          outcomeNext: row.result_outcome_next,
          outcomeUncertain: row.result_outcome_uncertain,
          outcomeVerified: row.result_outcome_verified,
          policyMode: row.result_policy_mode,
          resultText: row.result_text,
          runtimeCommand: row.result_runtime_command,
          status: row.result_status,
          workspaceWritePolicy: row.result_workspace_write_policy,
        })
      : "",
    createdAt: row.created_at,
  };
}

const sessionSelect = `SELECT
  id, repo, prompt, agent, parent_session_id, attempt, policy_mode, effective_permission_mode,
  workspace_write_policy, max_runtime_seconds, runtime_command, status, owner_id, branch, work_dir,
  result_text, outcome_headline, outcome_changed, outcome_verified, outcome_uncertain, outcome_next,
  error, failure_kind, claimed_at, heartbeat_at, started_at, finished_at,
  created_at, updated_at
FROM sessions`;

const watchSelect = `SELECT
  id, kind, repo, instructions, agent, policy_mode, workflow_name, branch_name, base_branch,
  head_branch, status,
  created_at, updated_at
FROM watches`;

function buildSessionFilters(query: SessionListQuery): { clauses: string[]; params: unknown[] } {
  const clauses: string[] = [];
  const params: unknown[] = [];

  if (query.status) {
    if (query.status === "active") {
      clauses.push(`status IN ('pending', 'running')`);
    } else {
      clauses.push("status = ?");
      params.push(query.status);
    }
  }

  if (query.agent) {
    clauses.push("agent = ?");
    params.push(query.agent);
  }

  if (query.parentSessionId !== undefined) {
    clauses.push("parent_session_id = ?");
    params.push(query.parentSessionId);
  }

  if (query.policyMode) {
    clauses.push("policy_mode = ?");
    params.push(query.policyMode);
  }

  return { clauses, params };
}

function buildWatchFilters(query: WatchListQuery): { clauses: string[]; params: unknown[] } {
  const clauses: string[] = [];
  const params: unknown[] = [];

  if (query.status) {
    clauses.push("status = ?");
    params.push(query.status);
  }

  if (query.kind) {
    clauses.push("kind = ?");
    params.push(query.kind);
  }

  if (query.repo) {
    clauses.push("repo = ?");
    params.push(query.repo);
  }

  return { clauses, params };
}

export class TeleCoderStore {
  private readonly db: Database;

  constructor(dbPath: string) {
    this.db = new Database(dbPath, { create: true, strict: true });
    this.migrate();
  }

  close(): void {
    this.db.close();
  }

  createWatch(input: {
    id: string;
    kind: WatchRecord["kind"];
    repo: string;
    instructions: string;
    agent?: string;
    policyMode?: WatchRecord["policyMode"];
    workflowName?: string;
    branchName?: string;
    baseBranch?: string;
    headBranch?: string;
    status?: WatchRecord["status"];
  }): WatchRecord {
    const createdAt = nowIso();
    const watch: WatchRecord = {
      id: input.id,
      kind: input.kind,
      repo: input.repo,
      instructions: input.instructions,
      agent: input.agent ?? "",
      policyMode: input.policyMode ?? "observe",
      workflowName: input.workflowName ?? "",
      branchName: input.branchName ?? "",
      baseBranch: input.baseBranch ?? "",
      headBranch: input.headBranch ?? "",
      status: input.status ?? "active",
      createdAt,
      updatedAt: createdAt,
    };

    this.db
      .query(
        `INSERT INTO watches (
          id, kind, repo, instructions, agent, policy_mode, workflow_name, branch_name, base_branch,
          head_branch, status, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .run(
        watch.id,
        watch.kind,
        watch.repo,
        watch.instructions,
        watch.agent,
        watch.policyMode,
        watch.workflowName,
        watch.branchName,
        watch.baseBranch,
        watch.headBranch,
        watch.status,
        watch.createdAt,
        watch.updatedAt,
      );

    return watch;
  }

  createSession(input: {
    agent: string;
    attempt?: number;
    branch?: string;
    claimedAt?: string;
    error?: string;
    failureKind?: SessionRecord["failureKind"];
    effectivePermissionMode?: SessionRecord["effectivePermissionMode"];
    finishedAt?: string;
    heartbeatAt?: string;
    id: string;
    maxRuntimeSeconds?: number;
    ownerId?: string;
    parentSessionId?: string;
    policyMode?: SessionRecord["policyMode"];
    prompt: string;
    repo: string;
    resultText?: string;
    outcomeChanged?: string;
    outcomeHeadline?: string;
    outcomeNext?: string;
    outcomeUncertain?: string;
    outcomeVerified?: string;
    runtimeCommand?: string;
    startedAt?: string;
    status?: SessionRecord["status"];
    workDir?: string;
    workspaceWritePolicy?: SessionRecord["workspaceWritePolicy"];
  }): SessionRecord {
    const createdAt = nowIso();
    const status = input.status ?? "pending";
    const resultText = input.resultText ?? "";
    const error = input.error ?? "";
    const derivedOutcome = deriveSessionOutcome({
      status,
      resultText,
      error,
    });
    const session: SessionRecord = {
      id: input.id,
      repo: input.repo,
      prompt: input.prompt,
      agent: input.agent,
      parentSessionId: input.parentSessionId ?? "",
      attempt: input.attempt ?? 1,
      policyMode: input.policyMode ?? "observe",
      effectivePermissionMode: input.effectivePermissionMode ?? "approve-reads",
      workspaceWritePolicy: input.workspaceWritePolicy ?? "blocked",
      maxRuntimeSeconds: input.maxRuntimeSeconds ?? 180,
      runtimeCommand: input.runtimeCommand ?? input.agent,
      status,
      ownerId: input.ownerId ?? "",
      branch: input.branch ?? "",
      workDir: input.workDir ?? "",
      resultText,
      outcomeHeadline: input.outcomeHeadline ?? derivedOutcome.outcomeHeadline,
      outcomeChanged: input.outcomeChanged ?? derivedOutcome.outcomeChanged,
      outcomeVerified: input.outcomeVerified ?? derivedOutcome.outcomeVerified,
      outcomeUncertain: input.outcomeUncertain ?? derivedOutcome.outcomeUncertain,
      outcomeNext: input.outcomeNext ?? derivedOutcome.outcomeNext,
      error,
      failureKind: input.failureKind ?? "",
      claimedAt: input.claimedAt ?? "",
      heartbeatAt: input.heartbeatAt ?? "",
      startedAt: input.startedAt ?? "",
      finishedAt: input.finishedAt ?? "",
      createdAt,
      updatedAt: createdAt,
    };

    this.db
      .query(
        `INSERT INTO sessions (
          id, repo, prompt, agent, parent_session_id, attempt, policy_mode, effective_permission_mode,
          workspace_write_policy, max_runtime_seconds, runtime_command, status, owner_id, branch,
          work_dir, result_text, outcome_headline, outcome_changed, outcome_verified,
          outcome_uncertain, outcome_next, error, failure_kind, claimed_at, heartbeat_at,
          started_at, finished_at, created_at, updated_at
        ) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
      )
      .run(
        session.id,
        session.repo,
        session.prompt,
        session.agent,
        session.parentSessionId,
        session.attempt,
        session.policyMode,
        session.effectivePermissionMode,
        session.workspaceWritePolicy,
        session.maxRuntimeSeconds,
        session.runtimeCommand,
        session.status,
        session.ownerId,
        session.branch,
        session.workDir,
        session.resultText,
        session.outcomeHeadline,
        session.outcomeChanged,
        session.outcomeVerified,
        session.outcomeUncertain,
        session.outcomeNext,
        session.error,
        session.failureKind,
        session.claimedAt,
        session.heartbeatAt,
        session.startedAt,
        session.finishedAt,
        session.createdAt,
        session.updatedAt,
      );

    return session;
  }

  getWatch(id: string): WatchRecord | null {
    const row = this.db.query(`${watchSelect} WHERE id = ?`).get(id) as WatchRow | null;
    if (!row) {
      return null;
    }
    return mapWatch(row);
  }

  listWatches(query: WatchListQuery = {}): WatchRecord[] {
    const { clauses, params } = buildWatchFilters(query);
    let sql = watchSelect;
    if (clauses.length > 0) {
      sql += ` WHERE ${clauses.join(" AND ")}`;
    }
    sql += " ORDER BY created_at DESC, id ASC";

    const rows = this.db.query(sql).all(...params) as WatchRow[];
    return rows.map(mapWatch);
  }

  getWatchRunByEventKey(watchId: string, eventKey: string): WatchRunRecord | null {
    const row = this.db
      .query(
        `SELECT
           wr.id, wr.watch_id, wr.session_id, wr.event_key, wr.source_ref, wr.trigger_summary,
           wr.created_at, s.status AS result_status, s.result_text, s.error AS result_error,
           s.outcome_headline AS result_outcome_headline,
           s.outcome_changed AS result_outcome_changed,
           s.outcome_verified AS result_outcome_verified,
           s.outcome_uncertain AS result_outcome_uncertain,
           s.outcome_next AS result_outcome_next,
           s.agent AS result_agent, s.runtime_command AS result_runtime_command,
           s.policy_mode AS result_policy_mode,
           s.workspace_write_policy AS result_workspace_write_policy
         FROM watch_runs wr
         LEFT JOIN sessions s ON s.id = wr.session_id
         WHERE wr.watch_id = ? AND wr.event_key = ?`,
      )
      .get(watchId, eventKey) as WatchRunRow | null;

    if (!row) {
      return null;
    }

    return mapWatchRun(row);
  }

  createWatchRun(input: {
    watchId: string;
    sessionId: string;
    eventKey: string;
    triggerSummary: string;
    sourceRef?: string;
  }): WatchRunRecord {
    const createdAt = nowIso();
    const result = this.db
      .query(
        `INSERT INTO watch_runs (
          watch_id, session_id, event_key, source_ref, trigger_summary, created_at
        ) VALUES (?, ?, ?, ?, ?, ?)`,
      )
      .run(
        input.watchId,
        input.sessionId,
        input.eventKey,
        input.sourceRef ?? "",
        input.triggerSummary,
        createdAt,
      );

    return {
      id: Number(result.lastInsertRowid),
      watchId: input.watchId,
      sessionId: input.sessionId,
      eventKey: input.eventKey,
      sourceRef: input.sourceRef ?? "",
      triggerSummary: input.triggerSummary,
      resultSummary: "",
      returnSummary: "",
      createdAt,
    };
  }

  reserveWatchRun(input: {
    watchId: string;
    eventKey: string;
    triggerSummary: string;
    sourceRef?: string;
  }): WatchRunRecord | null {
    const createdAt = nowIso();

    try {
      const result = this.db
        .query(
          `INSERT INTO watch_runs (
            watch_id, session_id, event_key, source_ref, trigger_summary, created_at
          ) VALUES (?, '', ?, ?, ?, ?)`,
        )
        .run(
          input.watchId,
          input.eventKey,
          input.sourceRef ?? "",
          input.triggerSummary,
          createdAt,
        );

      return {
        id: Number(result.lastInsertRowid),
        watchId: input.watchId,
        sessionId: "",
        eventKey: input.eventKey,
        sourceRef: input.sourceRef ?? "",
        triggerSummary: input.triggerSummary,
        resultSummary: "",
        returnSummary: "",
        createdAt,
      };
    } catch (error) {
      if (isUniqueConstraintError(error, "watch_runs.watch_id, watch_runs.event_key")) {
        return null;
      }
      throw error;
    }
  }

  attachWatchRunSession(id: number, sessionId: string): WatchRunRecord {
    const result = this.db
      .query(
        `UPDATE watch_runs
         SET session_id = ?
         WHERE id = ?`,
      )
      .run(sessionId, id);

    if (!result.changes) {
      throw new Error(`Watch run ${id} not found`);
    }

    const row = this.db
      .query(
        `SELECT
           wr.id, wr.watch_id, wr.session_id, wr.event_key, wr.source_ref, wr.trigger_summary,
           wr.created_at, s.status AS result_status, s.result_text, s.error AS result_error,
           s.outcome_headline AS result_outcome_headline,
           s.outcome_changed AS result_outcome_changed,
           s.outcome_verified AS result_outcome_verified,
           s.outcome_uncertain AS result_outcome_uncertain,
           s.outcome_next AS result_outcome_next,
           s.agent AS result_agent, s.runtime_command AS result_runtime_command,
           s.policy_mode AS result_policy_mode,
           s.workspace_write_policy AS result_workspace_write_policy
         FROM watch_runs wr
         LEFT JOIN sessions s ON s.id = wr.session_id
         WHERE wr.id = ?`,
      )
      .get(id) as WatchRunRow | null;

    if (!row) {
      throw new Error(`Watch run ${id} not found`);
    }

    return mapWatchRun(row);
  }

  deleteWatchRun(id: number): void {
    this.db.query("DELETE FROM watch_runs WHERE id = ?").run(id);
  }

  listWatchRuns(watchId: string): WatchRunRecord[] {
    const rows = this.db
      .query(
        `SELECT
           wr.id, wr.watch_id, wr.session_id, wr.event_key, wr.source_ref, wr.trigger_summary,
           wr.created_at, s.status AS result_status, s.result_text, s.error AS result_error,
           s.outcome_headline AS result_outcome_headline,
           s.outcome_changed AS result_outcome_changed,
           s.outcome_verified AS result_outcome_verified,
           s.outcome_uncertain AS result_outcome_uncertain,
           s.outcome_next AS result_outcome_next,
           s.agent AS result_agent, s.runtime_command AS result_runtime_command,
           s.policy_mode AS result_policy_mode,
           s.workspace_write_policy AS result_workspace_write_policy
         FROM watch_runs wr
         LEFT JOIN sessions s ON s.id = wr.session_id
         WHERE wr.watch_id = ?
         ORDER BY wr.id DESC`,
      )
      .all(watchId) as WatchRunRow[];

    return rows.map(mapWatchRun);
  }

  getSession(id: string): SessionRecord | null {
    const row = this.db.query(`${sessionSelect} WHERE id = ?`).get(id) as SessionRow | null;

    if (!row) {
      return null;
    }

    return mapSession(row);
  }

  listSessions(query: SessionListQuery = {}): SessionRecord[] {
    const { clauses, params } = buildSessionFilters(query);

    let sql = sessionSelect;
    const finalParams: unknown[] = [];

    if (query.lineageSessionId) {
      sql = `WITH RECURSIVE lineage(id) AS (
        SELECT id FROM sessions WHERE id = ?
        UNION
        SELECT s.parent_session_id
        FROM sessions s
        JOIN lineage l ON s.id = l.id
        WHERE s.parent_session_id != ''
        UNION
        SELECT s.id
        FROM sessions s
        JOIN lineage l ON s.parent_session_id = l.id
      )
      ${sessionSelect}
      WHERE id IN (SELECT id FROM lineage)`;
      finalParams.push(query.lineageSessionId);
    }

    if (clauses.length > 0) {
      sql += `${query.lineageSessionId ? " AND " : " WHERE "}${clauses.join(" AND ")}`;
      finalParams.push(...params);
    }

    sql += query.lineageSessionId
      ? " ORDER BY attempt ASC, created_at ASC, id ASC"
      : " ORDER BY created_at DESC";

    const rows = this.db.query(sql).all(...finalParams) as SessionRow[];

    return rows.map(mapSession);
  }

  listInboxSessions(limit = 20): SessionRecord[] {
    const safeLimit = Math.max(1, Math.min(limit, 200));
    const rows = this.db
      .query(
        `${sessionSelect}
         WHERE status IN ('complete', 'error')
         ORDER BY updated_at DESC, created_at DESC
         LIMIT ?`,
      )
      .all(safeLimit) as SessionRow[];

    return rows.map(mapSession);
  }

  updateSession(
    id: string,
    patch: Partial<
      Pick<
        SessionRecord,
        | "branch"
        | "claimedAt"
        | "error"
        | "failureKind"
        | "finishedAt"
        | "heartbeatAt"
        | "ownerId"
        | "resultText"
        | "startedAt"
        | "status"
        | "workDir"
      >
    >,
  ): SessionRecord {
    const current = this.getSession(id);
    if (!current) {
      throw new Error(`Session ${id} not found`);
    }

    const next: SessionRecord = {
      ...current,
      ...patch,
      updatedAt: nowIso(),
    };
    const derivedOutcome = deriveSessionOutcome({
      status: next.status,
      resultText: next.resultText,
      error: next.error,
    });
    next.outcomeHeadline = derivedOutcome.outcomeHeadline;
    next.outcomeChanged = derivedOutcome.outcomeChanged;
    next.outcomeVerified = derivedOutcome.outcomeVerified;
    next.outcomeUncertain = derivedOutcome.outcomeUncertain;
    next.outcomeNext = derivedOutcome.outcomeNext;

    this.db
      .query(
        `UPDATE sessions
         SET status = ?, owner_id = ?, branch = ?, work_dir = ?, result_text = ?,
             outcome_headline = ?, outcome_changed = ?, outcome_verified = ?,
             outcome_uncertain = ?, outcome_next = ?, error = ?, failure_kind = ?,
             claimed_at = ?, heartbeat_at = ?, started_at = ?, finished_at = ?, updated_at = ?
         WHERE id = ?`,
      )
      .run(
        next.status,
        next.ownerId,
        next.branch,
        next.workDir,
        next.resultText,
        next.outcomeHeadline,
        next.outcomeChanged,
        next.outcomeVerified,
        next.outcomeUncertain,
        next.outcomeNext,
        next.error,
        next.failureKind,
        next.claimedAt,
        next.heartbeatAt,
        next.startedAt,
        next.finishedAt,
        next.updatedAt,
        next.id,
      );

    return next;
  }

  claimPendingSession(id: string, ownerId: string, startedAt: string): SessionRecord | null {
    const claimedAt = startedAt;
    const result = this.db
      .query(
        `UPDATE sessions
         SET status = 'running',
             owner_id = ?,
             claimed_at = CASE WHEN claimed_at = '' THEN ? ELSE claimed_at END,
             heartbeat_at = ?,
             started_at = ?,
             finished_at = '',
             error = '',
             failure_kind = '',
             updated_at = ?
         WHERE id = ? AND status = 'pending'`,
      )
      .run(ownerId, claimedAt, startedAt, startedAt, startedAt, id);

    if (!result.changes) {
      return null;
    }

    return this.getSession(id);
  }

  insertEvent(input: {
    createdAt?: string;
    data: string;
    sessionId: string;
    type: SessionEventType;
  }): SessionEvent {
    const createdAt = input.createdAt ?? nowIso();
    const result = this.db
      .query(
        `INSERT INTO events (session_id, type, data, created_at)
         VALUES (?, ?, ?, ?)`,
      )
      .run(input.sessionId, input.type, input.data, createdAt);

    return {
      id: Number(result.lastInsertRowid),
      sessionId: input.sessionId,
      type: input.type,
      data: input.data,
      createdAt,
    };
  }

  listEvents(sessionId: string, afterId = 0): SessionEvent[] {
    const rows = this.db
      .query(
        `SELECT id, session_id, type, data, created_at
         FROM events
         WHERE session_id = ? AND id > ?
         ORDER BY id ASC`,
      )
      .all(sessionId, afterId) as EventRow[];

    return rows.map((row) => ({
      id: row.id,
      sessionId: row.session_id,
      type: row.type,
      data: row.data,
      createdAt: row.created_at,
    }));
  }

  recoverPendingSessions(ownerId: string, message: string): SessionRecord[] {
    const rows = this.db
      .query(
        `${sessionSelect}
         WHERE status = 'pending'
         ORDER BY created_at ASC`,
      )
      .all() as SessionRow[];

    if (rows.length === 0) {
      return [];
    }

    const recoveredAt = nowIso();
    const recoveredRows: SessionRow[] = [];
    const updateStatement = this.db.query(
      `UPDATE sessions
       SET owner_id = ?, claimed_at = ?, heartbeat_at = ?, updated_at = ?
       WHERE id = ? AND status = 'pending'`,
    );
    const insertEventStatement = this.db.query(
      `INSERT INTO events (session_id, type, data, created_at)
       VALUES (?, 'status', ?, ?)`,
    );

    const transaction = this.db.transaction((pendingRows: SessionRow[]) => {
      for (const row of pendingRows) {
        const result = updateStatement.run(ownerId, recoveredAt, recoveredAt, recoveredAt, row.id);
        if (!result.changes) {
          continue;
        }

        insertEventStatement.run(row.id, message, recoveredAt);
        recoveredRows.push({
          ...row,
          owner_id: ownerId,
          claimed_at: recoveredAt,
          heartbeat_at: recoveredAt,
          updated_at: recoveredAt,
        });
      }
    });
    transaction(rows);

    return recoveredRows.map(mapSession);
  }

  reconcileStaleRunningSessions(cutoff: string, message: string): SessionRecord[] {
    const rows = this.db
      .query(
        `${sessionSelect}
         WHERE status = 'running'
           AND COALESCE(NULLIF(heartbeat_at, ''), NULLIF(claimed_at, ''), updated_at, created_at) < ?
         ORDER BY created_at ASC`,
      )
      .all(cutoff) as SessionRow[];

    if (rows.length === 0) {
      return [];
    }

    const reconciledAt = nowIso();
    const updateStatement = this.db.query(
      `UPDATE sessions
       SET status = 'error', error = ?, failure_kind = 'abandoned', finished_at = ?, updated_at = ?
       WHERE id = ?`,
    );
    const insertEventStatement = this.db.query(
      `INSERT INTO events (session_id, type, data, created_at)
       VALUES (?, 'error', ?, ?)`,
    );

    const transaction = this.db.transaction((staleRows: SessionRow[]) => {
      for (const row of staleRows) {
        updateStatement.run(message, reconciledAt, reconciledAt, row.id);
        insertEventStatement.run(row.id, message, reconciledAt);
      }
    });
    transaction(rows);

    return rows.map((row) =>
      mapSession({
        ...row,
        status: "error",
        error: message,
        failure_kind: "abandoned",
        finished_at: reconciledAt,
        updated_at: reconciledAt,
      }),
    );
  }

  private migrate(): void {
    this.db.exec("PRAGMA journal_mode = WAL");

    const versionRow = this.db.query("PRAGMA user_version").get() as { user_version: number };
    const version = versionRow.user_version;

    if (version < 1) {
      this.db.exec(`
        CREATE TABLE IF NOT EXISTS sessions (
          id TEXT PRIMARY KEY,
          repo TEXT NOT NULL,
          prompt TEXT NOT NULL,
          agent TEXT NOT NULL,
          parent_session_id TEXT NOT NULL DEFAULT '',
          attempt INTEGER NOT NULL DEFAULT 1,
          policy_mode TEXT NOT NULL DEFAULT 'observe',
          effective_permission_mode TEXT NOT NULL DEFAULT 'approve-reads',
          workspace_write_policy TEXT NOT NULL DEFAULT 'blocked',
          max_runtime_seconds INTEGER NOT NULL DEFAULT 180,
          runtime_command TEXT NOT NULL DEFAULT '',
          status TEXT NOT NULL,
          owner_id TEXT NOT NULL DEFAULT '',
          branch TEXT NOT NULL DEFAULT '',
          work_dir TEXT NOT NULL DEFAULT '',
          result_text TEXT NOT NULL DEFAULT '',
          outcome_headline TEXT NOT NULL DEFAULT '',
          outcome_changed TEXT NOT NULL DEFAULT '',
          outcome_verified TEXT NOT NULL DEFAULT '',
          outcome_uncertain TEXT NOT NULL DEFAULT '',
          outcome_next TEXT NOT NULL DEFAULT '',
          error TEXT NOT NULL DEFAULT '',
          failure_kind TEXT NOT NULL DEFAULT '',
          claimed_at TEXT NOT NULL DEFAULT '',
          heartbeat_at TEXT NOT NULL DEFAULT '',
          started_at TEXT NOT NULL DEFAULT '',
          finished_at TEXT NOT NULL DEFAULT '',
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS events (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          session_id TEXT NOT NULL,
          type TEXT NOT NULL,
          data TEXT NOT NULL,
          created_at TEXT NOT NULL,
          FOREIGN KEY(session_id) REFERENCES sessions(id) ON DELETE CASCADE
        );

        CREATE INDEX IF NOT EXISTS idx_events_session_id_id ON events(session_id, id);
        PRAGMA user_version = 1;
      `);
    }

    if (version < 2) {
      this.ensureSessionColumn("owner_id", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("claimed_at", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("heartbeat_at", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("started_at", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("finished_at", "TEXT NOT NULL DEFAULT ''");
      this.db.exec(`
        CREATE INDEX IF NOT EXISTS idx_sessions_status_updated_at ON sessions(status, updated_at);
        PRAGMA user_version = 2;
      `);
    }

    if (version < 3) {
      this.ensureSessionColumn("parent_session_id", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("attempt", "INTEGER NOT NULL DEFAULT 1");
      this.db.exec(`
        CREATE INDEX IF NOT EXISTS idx_sessions_parent_session_id ON sessions(parent_session_id);
        PRAGMA user_version = 3;
      `);
    }

    if (version < 4) {
      this.ensureSessionColumn("policy_mode", "TEXT NOT NULL DEFAULT 'observe'");
      this.ensureSessionColumn(
        "effective_permission_mode",
        "TEXT NOT NULL DEFAULT 'approve-reads'",
      );
      this.ensureSessionColumn(
        "workspace_write_policy",
        "TEXT NOT NULL DEFAULT 'blocked'",
      );
      this.ensureSessionColumn("max_runtime_seconds", "INTEGER NOT NULL DEFAULT 180");
      this.ensureSessionColumn("runtime_command", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("failure_kind", "TEXT NOT NULL DEFAULT ''");
      this.db.exec(`
        CREATE INDEX IF NOT EXISTS idx_sessions_policy_mode ON sessions(policy_mode);
        PRAGMA user_version = 4;
      `);
    }

    if (version < 5) {
      this.db.exec(`
        CREATE TABLE IF NOT EXISTS watches (
          id TEXT PRIMARY KEY,
          kind TEXT NOT NULL,
          repo TEXT NOT NULL,
          instructions TEXT NOT NULL,
          agent TEXT NOT NULL DEFAULT '',
          policy_mode TEXT NOT NULL DEFAULT 'observe',
          workflow_name TEXT NOT NULL DEFAULT '',
          branch_name TEXT NOT NULL DEFAULT '',
          base_branch TEXT NOT NULL DEFAULT '',
          head_branch TEXT NOT NULL DEFAULT '',
          status TEXT NOT NULL DEFAULT 'active',
          created_at TEXT NOT NULL,
          updated_at TEXT NOT NULL
        );

        CREATE TABLE IF NOT EXISTS watch_runs (
          id INTEGER PRIMARY KEY AUTOINCREMENT,
          watch_id TEXT NOT NULL,
          session_id TEXT NOT NULL,
          event_key TEXT NOT NULL,
          source_ref TEXT NOT NULL DEFAULT '',
          trigger_summary TEXT NOT NULL,
          created_at TEXT NOT NULL,
          UNIQUE(watch_id, event_key)
        );

        CREATE INDEX IF NOT EXISTS idx_watches_status_kind ON watches(status, kind);
        CREATE INDEX IF NOT EXISTS idx_watch_runs_watch_id_id ON watch_runs(watch_id, id);
        PRAGMA user_version = 5;
      `);
    }

    if (version < 6) {
      this.ensureWatchColumn("base_branch", "TEXT NOT NULL DEFAULT ''");
      this.ensureWatchColumn("head_branch", "TEXT NOT NULL DEFAULT ''");
      this.db.exec(`
        PRAGMA user_version = 6;
      `);
    }

    if (version < 7) {
      this.ensureSessionColumn("outcome_headline", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("outcome_changed", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("outcome_verified", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("outcome_uncertain", "TEXT NOT NULL DEFAULT ''");
      this.ensureSessionColumn("outcome_next", "TEXT NOT NULL DEFAULT ''");
      this.db.exec(`
        PRAGMA user_version = 7;
      `);
    }
  }

  private ensureSessionColumn(name: string, definition: string): void {
    const columns = this.db.query("PRAGMA table_info(sessions)").all() as Array<{ name: string }>;
    if (columns.some((column) => column.name === name)) {
      return;
    }
    this.db.exec(`ALTER TABLE sessions ADD COLUMN ${name} ${definition}`);
  }

  private ensureWatchColumn(name: string, definition: string): void {
    const columns = this.db.query("PRAGMA table_info(watches)").all() as Array<{ name: string }>;
    if (columns.some((column) => column.name === name)) {
      return;
    }
    this.db.exec(`ALTER TABLE watches ADD COLUMN ${name} ${definition}`);
  }
}
