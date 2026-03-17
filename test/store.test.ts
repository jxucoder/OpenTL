import { afterEach, expect, test } from "bun:test";
import { mkdtemp, rm } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { TeleCoderStore } from "../src/store.ts";

const cleanup: string[] = [];

afterEach(async () => {
  while (cleanup.length > 0) {
    const path = cleanup.pop()!;
    await rm(path, { force: true, recursive: true });
  }
});

test("store persists sessions and events across reopen", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-store-"));
  cleanup.push(dir);
  const dbPath = join(dir, "telecoder.sqlite");
  const claimedAt = "2026-03-16T04:30:00.000Z";
  const startedAt = "2026-03-16T04:31:00.000Z";
  const finishedAt = "2026-03-16T04:32:00.000Z";

  const store = new TeleCoderStore(dbPath);
  const session = store.createSession({
    id: "sess-1",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "opencode",
    parentSessionId: "parent-0",
    attempt: 3,
    ownerId: "owner-1",
    claimedAt,
    heartbeatAt: claimedAt,
  });
  const event = store.insertEvent({
    sessionId: session.id,
    type: "status",
    data: "running acpx",
  });
  store.updateSession(session.id, {
    status: "complete",
    resultText: "done",
    startedAt,
    finishedAt,
  });
  store.close();

  const reopened = new TeleCoderStore(dbPath);
  const loaded = reopened.getSession(session.id);
  const events = reopened.listEvents(session.id);
  reopened.close();

  expect(loaded?.status).toBe("complete");
  expect(loaded?.resultText).toBe("done");
  expect(loaded?.outcomeHeadline).toBe("done");
  expect(loaded?.outcomeChanged).toBe("done");
  expect(loaded?.parentSessionId).toBe("parent-0");
  expect(loaded?.attempt).toBe(3);
  expect(loaded?.policyMode).toBe("observe");
  expect(loaded?.effectivePermissionMode).toBe("approve-reads");
  expect(loaded?.workspaceWritePolicy).toBe("blocked");
  expect(loaded?.maxRuntimeSeconds).toBe(180);
  expect(loaded?.runtimeCommand).toBe("opencode");
  expect(loaded?.ownerId).toBe("owner-1");
  expect(loaded?.claimedAt).toBe(claimedAt);
  expect(loaded?.startedAt).toBe(startedAt);
  expect(loaded?.finishedAt).toBe(finishedAt);
  expect(events).toHaveLength(1);
  expect(events[0]?.id).toBe(event.id);
  expect(events[0]?.data).toBe("running acpx");
});

test("store lists inbox sessions with structured outcomes", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-store-"));
  cleanup.push(dir);
  const dbPath = join(dir, "telecoder.sqlite");

  const store = new TeleCoderStore(dbPath);
  store.createSession({
    id: "pending",
    repo: "git@example.com/repo.git",
    prompt: "pending",
    agent: "codex",
    status: "pending",
  });
  store.createSession({
    id: "complete",
    repo: "git@example.com/repo.git",
    prompt: "complete",
    agent: "codex",
    status: "complete",
    resultText: "Changed: Fixed the flaky test.\nVerified: Ran unit tests.\nNext: Monitor CI.",
  });
  store.createSession({
    id: "error",
    repo: "git@example.com/repo.git",
    prompt: "error",
    agent: "codex",
    status: "error",
    error: "approval denied",
  });

  const inbox = store.listInboxSessions(10);
  const limited = store.listInboxSessions(1);
  store.close();

  expect(inbox.map((session) => session.id).sort()).toEqual(["complete", "error"]);
  expect(inbox.find((session) => session.id === "complete")?.outcomeChanged).toBe(
    "Fixed the flaky test.",
  );
  expect(inbox.find((session) => session.id === "complete")?.outcomeVerified).toBe(
    "Ran unit tests.",
  );
  expect(inbox.find((session) => session.id === "error")?.outcomeHeadline).toContain("Failed:");
  expect(limited).toHaveLength(1);
});

test("store lists sessions with status, parent, agent, policy, and lineage filters", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-store-"));
  cleanup.push(dir);
  const dbPath = join(dir, "telecoder.sqlite");

  const store = new TeleCoderStore(dbPath);
  store.createSession({
    id: "root",
    repo: "git@example.com/repo.git",
    prompt: "root",
    agent: "codex",
    status: "complete",
    attempt: 1,
    policyMode: "standard",
    effectivePermissionMode: "approve-all",
    workspaceWritePolicy: "contained",
    maxRuntimeSeconds: 300,
  });
  store.createSession({
    id: "child-1",
    repo: "git@example.com/repo.git",
    prompt: "child-1",
    agent: "codex",
    parentSessionId: "root",
    status: "error",
    attempt: 2,
    policyMode: "locked",
    effectivePermissionMode: "deny-all",
    workspaceWritePolicy: "blocked",
    maxRuntimeSeconds: 60,
  });
  store.createSession({
    id: "child-2",
    repo: "git@example.com/repo.git",
    prompt: "child-2",
    agent: "claude",
    parentSessionId: "child-1",
    status: "running",
    attempt: 3,
    policyMode: "observe",
  });
  store.createSession({
    id: "sibling",
    repo: "git@example.com/repo.git",
    prompt: "sibling",
    agent: "opencode",
    parentSessionId: "root",
    status: "pending",
    attempt: 2,
    policyMode: "observe",
  });
  store.createSession({
    id: "unrelated",
    repo: "git@example.com/other.git",
    prompt: "unrelated",
    agent: "codex",
    status: "complete",
    attempt: 1,
    policyMode: "standard",
    effectivePermissionMode: "approve-all",
    workspaceWritePolicy: "contained",
    maxRuntimeSeconds: 300,
  });

  const complete = store.listSessions({ status: "complete" }).map((session) => session.id).sort();
  const active = store.listSessions({ status: "active" }).map((session) => session.id).sort();
  const children = store
    .listSessions({ parentSessionId: "root" })
    .map((session) => session.id)
    .sort();
  const claude = store.listSessions({ agent: "claude" }).map((session) => session.id);
  const locked = store.listSessions({ policyMode: "locked" }).map((session) => session.id);
  const lineage = store
    .listSessions({ lineageSessionId: "child-2" })
    .map((session) => `${session.id}:${session.attempt}`);
  const lineageChildren = store
    .listSessions({ lineageSessionId: "root", parentSessionId: "root" })
    .map((session) => session.id)
    .sort();
  store.close();

  expect(complete).toEqual(["root", "unrelated"]);
  expect(active).toEqual(["child-2", "sibling"]);
  expect(children).toEqual(["child-1", "sibling"]);
  expect(claude).toEqual(["child-2"]);
  expect(locked).toEqual(["child-1"]);
  expect(lineage).toEqual(["root:1", "child-1:2", "sibling:2", "child-2:3"]);
  expect(lineageChildren).toEqual(["child-1", "sibling"]);
});

test("store claims a pending session exactly once", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-store-"));
  cleanup.push(dir);
  const dbPath = join(dir, "telecoder.sqlite");

  const store = new TeleCoderStore(dbPath);
  store.createSession({
    id: "claimable",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "claude",
    attempt: 1,
    ownerId: "owner-1",
    claimedAt: "2026-03-16T04:00:00.000Z",
    heartbeatAt: "2026-03-16T04:00:05.000Z",
  });

  const startedAt = "2026-03-16T04:10:00.000Z";
  const claimed = store.claimPendingSession("claimable", "owner-2", startedAt);
  const secondClaim = store.claimPendingSession("claimable", "owner-3", "2026-03-16T04:12:00.000Z");
  const session = store.getSession("claimable");
  store.close();

  expect(claimed?.status).toBe("running");
  expect(claimed?.attempt).toBe(1);
  expect(claimed?.ownerId).toBe("owner-2");
  expect(claimed?.claimedAt).toBe("2026-03-16T04:00:00.000Z");
  expect(claimed?.startedAt).toBe(startedAt);
  expect(claimed?.heartbeatAt).toBe(startedAt);
  expect(secondClaim).toBeNull();
  expect(session?.status).toBe("running");
  expect(session?.ownerId).toBe("owner-2");
});

test("store recovers pending sessions for restart", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-store-"));
  cleanup.push(dir);
  const dbPath = join(dir, "telecoder.sqlite");

  const store = new TeleCoderStore(dbPath);
  store.createSession({
    id: "stale-pending",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "claude",
    parentSessionId: "root-session",
    attempt: 2,
    ownerId: "owner-stale",
    claimedAt: "2026-03-16T04:00:00.000Z",
    heartbeatAt: "2026-03-16T04:00:05.000Z",
  });
  store.createSession({
    id: "fresh-running",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "codex",
    status: "running",
    ownerId: "owner-fresh",
    claimedAt: "2099-03-16T04:00:00.000Z",
    heartbeatAt: "2099-03-16T04:00:05.000Z",
  });

  const recovered = store.recoverPendingSessions("owner-new", "Recovered pending session after restart; requeued.");
  const pending = store.getSession("stale-pending");
  const pendingEvents = store.listEvents("stale-pending");
  const running = store.getSession("fresh-running");
  store.close();

  expect(recovered.map((session) => session.id)).toEqual(["stale-pending"]);
  expect(pending?.status).toBe("pending");
  expect(pending?.parentSessionId).toBe("root-session");
  expect(pending?.attempt).toBe(2);
  expect(pending?.ownerId).toBe("owner-new");
  expect(pending?.claimedAt).not.toBe("2026-03-16T04:00:00.000Z");
  expect(pending?.heartbeatAt).toBe(pending?.claimedAt);
  expect(running?.status).toBe("running");
  expect(pendingEvents).toHaveLength(1);
  expect(pendingEvents[0]?.type).toBe("status");
  expect(pendingEvents[0]?.data).toBe("Recovered pending session after restart; requeued.");
});

test("store reconciles stale running sessions", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-store-"));
  cleanup.push(dir);
  const dbPath = join(dir, "telecoder.sqlite");

  const store = new TeleCoderStore(dbPath);
  store.createSession({
    id: "stale-running",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "claude",
    status: "running",
    ownerId: "owner-stale",
    claimedAt: "2026-03-16T04:00:00.000Z",
    heartbeatAt: "2026-03-16T04:00:05.000Z",
  });
  store.createSession({
    id: "stale-pending",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "codex",
    ownerId: "owner-pending",
    claimedAt: "2026-03-16T04:00:00.000Z",
    heartbeatAt: "2026-03-16T04:00:05.000Z",
  });

  const recovered = store.reconcileStaleRunningSessions(
    "2026-03-16T04:10:00.000Z",
    "stale session recovered",
  );

  const stale = store.getSession("stale-running");
  const pending = store.getSession("stale-pending");
  const staleEvents = store.listEvents("stale-running");
  store.close();

  expect(recovered.map((session) => session.id)).toEqual(["stale-running"]);
  expect(stale?.status).toBe("error");
  expect(stale?.error).toBe("stale session recovered");
  expect(stale?.finishedAt).not.toBe("");
  expect(stale?.failureKind).toBe("abandoned");
  expect(pending?.status).toBe("pending");
  expect(staleEvents).toHaveLength(1);
  expect(staleEvents[0]?.type).toBe("error");
  expect(staleEvents[0]?.data).toBe("stale session recovered");
});

test("store persists watches and watch runs across reopen", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-store-"));
  cleanup.push(dir);
  const dbPath = join(dir, "telecoder.sqlite");

  const store = new TeleCoderStore(dbPath);
  const watch = store.createWatch({
    id: "watch-1",
    kind: "ci_failure",
    repo: "git@example.com/repo.git",
    instructions: "Investigate the CI failure",
    policyMode: "observe",
    workflowName: "build",
    branchName: "main",
  });
  store.createSession({
    id: "watch-session",
    repo: watch.repo,
    prompt: "hello",
    agent: "codex",
    status: "complete",
    resultText: "Likely test regression",
  });
  store.createWatchRun({
    watchId: watch.id,
    sessionId: "watch-session",
    eventKey: "run-123",
    triggerSummary: "CI watch fired: build on main",
    sourceRef: "https://ci.example/run/123",
  });
  store.close();

  const reopened = new TeleCoderStore(dbPath);
  const loadedWatch = reopened.getWatch(watch.id);
  const runs = reopened.listWatchRuns(watch.id);
  reopened.close();

  expect(loadedWatch?.workflowName).toBe("build");
  expect(loadedWatch?.branchName).toBe("main");
  expect(runs).toHaveLength(1);
  expect(runs[0]?.eventKey).toBe("run-123");
  expect(runs[0]?.sourceRef).toBe("https://ci.example/run/123");
  expect(runs[0]?.resultSummary).toContain("Likely test regression");
  expect(runs[0]?.returnSummary).toContain("Runtime: acpx -> codex");
});
