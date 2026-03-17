import { afterEach, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { TeleCoderEngine } from "../src/engine.ts";
import { runCommand } from "../src/process.ts";
import { TaskRuntimeError } from "../src/runtime/acpx.ts";
import { TeleCoderStore } from "../src/store.ts";
import type { TeleCoderConfig } from "../src/types.ts";

const cleanup: string[] = [];

afterEach(async () => {
  while (cleanup.length > 0) {
    const path = cleanup.pop()!;
    await rm(path, { force: true, recursive: true });
  }
});

function makeConfig(root: string): TeleCoderConfig {
  return {
    dataDir: root,
    dbPath: join(root, "telecoder.sqlite"),
    workspaceDir: join(root, "workspaces"),
    listenHost: "127.0.0.1",
    listenPort: 7080,
    acpxCommand: "acpx",
    agentCommands: {},
    defaultAgent: "codex",
    defaultPolicyMode: "observe",
    permissionMode: "approve-reads",
    workspaceWritePolicy: "blocked",
    policyMaxRuntimeSeconds: 180,
    promptTimeoutSeconds: 300,
    sessionHeartbeatSeconds: 5,
    sessionStaleSeconds: 30,
  };
}

async function createRepoFixture(root: string): Promise<string> {
  const repoDir = join(root, "source-repo");
  await mkdir(repoDir, { recursive: true });
  await writeFile(join(repoDir, "README.md"), "# test\n");

  const initResult = await runCommand(["git", "init"], { cwd: repoDir });
  if (initResult.exitCode !== 0) {
    throw new Error(initResult.stderr || initResult.stdout || "git init failed");
  }

  const addResult = await runCommand(["git", "add", "README.md"], { cwd: repoDir });
  if (addResult.exitCode !== 0) {
    throw new Error(addResult.stderr || addResult.stdout || "git add failed");
  }

  const commitResult = await runCommand(
    [
      "git",
      "-c",
      "user.name=TeleCoder Test",
      "-c",
      "user.email=test@example.com",
      "commit",
      "-m",
      "init",
    ],
    { cwd: repoDir },
  );
  if (commitResult.exitCode !== 0) {
    throw new Error(commitResult.stderr || commitResult.stdout || "git commit failed");
  }

  return repoDir;
}

async function createPrFixture(root: string): Promise<{
  baseBranch: string;
  headBranch: string;
  headSha: string;
  repo: string;
}> {
  const repo = await createRepoFixture(root);
  const baseBranchResult = await runCommand(["git", "branch", "--show-current"], { cwd: repo });
  if (baseBranchResult.exitCode !== 0) {
    throw new Error(baseBranchResult.stderr || baseBranchResult.stdout || "git branch failed");
  }

  const baseBranch = baseBranchResult.stdout || "main";
  const headBranch = "feature/pr-watch";
  const checkoutResult = await runCommand(["git", "checkout", "-b", headBranch], { cwd: repo });
  if (checkoutResult.exitCode !== 0) {
    throw new Error(checkoutResult.stderr || checkoutResult.stdout || "git checkout failed");
  }

  await writeFile(join(repo, "README.md"), "# test\n\nPR change\n");
  await writeFile(join(repo, "src.txt"), "new file\n");

  const addResult = await runCommand(["git", "add", "README.md", "src.txt"], { cwd: repo });
  if (addResult.exitCode !== 0) {
    throw new Error(addResult.stderr || addResult.stdout || "git add failed");
  }

  const commitResult = await runCommand(
    [
      "git",
      "-c",
      "user.name=TeleCoder Test",
      "-c",
      "user.email=test@example.com",
      "commit",
      "-m",
      "feature change",
    ],
    { cwd: repo },
  );
  if (commitResult.exitCode !== 0) {
    throw new Error(commitResult.stderr || commitResult.stdout || "git commit failed");
  }

  const headShaResult = await runCommand(["git", "rev-parse", "HEAD"], { cwd: repo });
  if (headShaResult.exitCode !== 0) {
    throw new Error(headShaResult.stderr || headShaResult.stdout || "git rev-parse failed");
  }

  const resetBranchResult = await runCommand(["git", "checkout", baseBranch], { cwd: repo });
  if (resetBranchResult.exitCode !== 0) {
    throw new Error(
      resetBranchResult.stderr || resetBranchResult.stdout || "git checkout base failed",
    );
  }

  return {
    repo,
    baseBranch,
    headBranch,
    headSha: headShaResult.stdout,
  };
}

async function waitForSession(
  engine: TeleCoderEngine,
  sessionId: string,
  predicate: (status: string) => boolean,
): Promise<void> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    const session = engine.getSession(sessionId);
    if (session && predicate(session.status)) {
      return;
    }
    await Bun.sleep(20);
  }

  throw new Error(`Timed out waiting for session ${sessionId}`);
}

test("engine recovers stale unfinished sessions on startup", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "stale-session",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "claude",
    status: "running",
    ownerId: "dead-owner",
    claimedAt: "2000-01-01T00:00:00.000Z",
    heartbeatAt: "2000-01-01T00:00:01.000Z",
  });

  const engine = new TeleCoderEngine(makeConfig(dir), store, {
    async runTask() {
      return { output: "" };
    },
  });

  const session = engine.getSession("stale-session");
  const events = engine.listEvents("stale-session");
  engine.close();

  expect(session?.status).toBe("error");
  expect(session?.error).toContain("without heartbeat");
  expect(session?.finishedAt).not.toBe("");
  expect(events).toHaveLength(1);
  expect(events[0]?.type).toBe("error");
});

test("engine requeues pending sessions on startup", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "pending-session",
    repo,
    prompt: "hello",
    agent: "claude",
    ownerId: "dead-owner",
    claimedAt: "2000-01-01T00:00:00.000Z",
    heartbeatAt: "2000-01-01T00:00:01.000Z",
  });

  const engine = new TeleCoderEngine(makeConfig(dir), store, {
    async runTask() {
      return { output: "recovered output" };
    },
  });

  await waitForSession(engine, "pending-session", (status) => status === "complete");

  const session = engine.getSession("pending-session");
  const events = engine.listEvents("pending-session");
  engine.close();

  expect(session?.status).toBe("complete");
  expect(session?.resultText).toBe("recovered output");
  expect(session?.outcomeHeadline).toBe("recovered output");
  expect(session?.outcomeChanged).toBe("recovered output");
  expect(session?.policyMode).toBe("observe");
  expect(session?.startedAt).not.toBe("");
  expect(events[0]?.type).toBe("status");
  expect(events[0]?.data).toBe("Recovered pending session after restart; requeued.");
  expect(events.some((event) => event.type === "done")).toBe(true);
});

test("engine reruns a finished session with explicit lineage", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "finished-session",
    repo,
    prompt: "hello again",
    agent: "claude",
    status: "complete",
    attempt: 1,
    ownerId: "owner-1",
    claimedAt: "2026-03-16T04:00:00.000Z",
    heartbeatAt: "2026-03-16T04:00:01.000Z",
    startedAt: "2026-03-16T04:00:02.000Z",
    finishedAt: "2026-03-16T04:00:03.000Z",
    resultText: "old output",
  });

  const engine = new TeleCoderEngine(makeConfig(dir), store, {
    async runTask() {
      return { output: "rerun output" };
    },
  });

  const rerun = await engine.rerunSession("finished-session");
  await waitForSession(engine, rerun.id, (status) => status === "complete");

  const session = engine.getSession(rerun.id);
  const events = engine.listEvents(rerun.id);
  engine.close();

  expect(session?.parentSessionId).toBe("finished-session");
  expect(session?.attempt).toBe(2);
  expect(session?.policyMode).toBe("observe");
  expect(session?.status).toBe("complete");
  expect(session?.resultText).toBe("rerun output");
  expect(session?.outcomeHeadline).toBe("rerun output");
  expect(events[0]?.type).toBe("status");
  expect(events[0]?.data).toBe("Rerun requested from session finished-session");
});

test("engine rejects rerun of an active session", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "running-session",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "claude",
    status: "running",
    attempt: 1,
    ownerId: "owner-1",
    claimedAt: "2026-03-16T04:00:00.000Z",
    heartbeatAt: "2099-03-16T04:00:01.000Z",
  });

  const engine = new TeleCoderEngine(makeConfig(dir), store, {
    async runTask() {
      return { output: "" };
    },
  });

  await expect(engine.rerunSession("running-session")).rejects.toThrow(
    "Session running-session is still active and cannot be rerun",
  );
  engine.close();
});

test("engine records requested policy and runtime metadata", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);
  const runtimeCalls: Array<{ permissionMode: string; timeoutSeconds: number }> = [];

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask(input) {
      runtimeCalls.push({
        permissionMode: input.permissionMode,
        timeoutSeconds: input.timeoutSeconds,
      });
      return { output: "policy output" };
    },
  });

  const session = await engine.createTask({
    repo,
    prompt: "hello",
    agent: "claude",
    policyMode: "standard",
  });
  await waitForSession(engine, session.id, (status) => status === "complete");

  const loaded = engine.getSession(session.id);
  const events = engine.listEvents(session.id);
  engine.close();

  expect(loaded?.policyMode).toBe("standard");
  expect(loaded?.effectivePermissionMode).toBe("approve-all");
  expect(loaded?.workspaceWritePolicy).toBe("contained");
  expect(loaded?.maxRuntimeSeconds).toBe(300);
  expect(loaded?.runtimeCommand).toBe("claude");
  expect(loaded?.outcomeHeadline).toBe("policy output");
  expect(runtimeCalls).toEqual([
    {
      permissionMode: "approve-all",
      timeoutSeconds: 300,
    },
  ]);
  expect(events[0]?.data).toContain("policy standard");
});

test("engine records policy-denied runtime failures", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      throw new TaskRuntimeError("permission denied by runtime", "policy_denied", 1);
    },
  });

  const session = await engine.createTask({
    repo,
    prompt: "hello",
    policyMode: "locked",
  });
  await waitForSession(engine, session.id, (status) => status === "error");

  const loaded = engine.getSession(session.id);
  engine.close();

  expect(loaded?.status).toBe("error");
  expect(loaded?.failureKind).toBe("policy_denied");
  expect(loaded?.error).toContain("permission denied");
  expect(loaded?.outcomeHeadline).toContain("Failed:");
  expect(loaded?.outcomeUncertain).toContain("permission denied");
});

test("engine triggers matching CI watches and records run history", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "Investigated failing test suite" };
    },
  });

  const watch = engine.createCiWatch({
    repo,
    instructions: "Investigate the failing build.",
    workflowName: "build",
    branchName: "main",
    policyMode: "observe",
  });

  const triggered = await engine.triggerCiWatchEvent({
    repo,
    workflowName: "build",
    branchName: "main",
    runId: "run-123",
    runUrl: "https://ci.example/run/123",
    sha: "abcdef1234567890",
    status: "completed",
    conclusion: "failure",
    summary: "Unit tests failed",
  });

  expect(triggered).toHaveLength(1);
  await waitForSession(engine, triggered[0]!.session.id, (status) => status === "complete");

  const session = engine.getSession(triggered[0]!.session.id);
  const runs = engine.listWatchRuns(watch.id);
  engine.close();

  expect(session?.policyMode).toBe("observe");
  expect(session?.prompt).toContain("Workflow: build");
  expect(runs).toHaveLength(1);
  expect(runs[0]?.triggerSummary).toContain("CI watch");
  expect(runs[0]?.resultSummary).toContain("Investigated failing test suite");
});

test("engine ignores duplicate and non-failing CI watch events", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "Investigated" };
    },
  });

  const watch = engine.createCiWatch({
    repo,
    instructions: "Investigate the failing build.",
    workflowName: "build",
  });

  const ignored = await engine.triggerCiWatchEvent({
    repo,
    workflowName: "build",
    branchName: "main",
    runId: "run-ok",
    status: "completed",
    conclusion: "success",
  });
  const first = await engine.triggerCiWatchEvent({
    repo,
    workflowName: "build",
    branchName: "main",
    runId: "run-dup",
    status: "completed",
    conclusion: "failure",
  });
  const duplicate = await engine.triggerCiWatchEvent({
    repo,
    workflowName: "build",
    branchName: "main",
    runId: "run-dup",
    status: "completed",
    conclusion: "failure",
  });

  await waitForSession(engine, first[0]!.session.id, (status) => status === "complete");
  const runs = engine.listWatchRuns(watch.id);
  engine.close();

  expect(ignored).toHaveLength(0);
  expect(first).toHaveLength(1);
  expect(duplicate).toHaveLength(0);
  expect(runs).toHaveLength(1);
});

test("engine coalesces concurrent duplicate CI watch events into one session", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "Investigated duplicate delivery" };
    },
  });

  const watch = engine.createCiWatch({
    repo,
    instructions: "Investigate the failing build.",
    workflowName: "build",
  });

  const event = {
    repo,
    workflowName: "build",
    branchName: "main",
    runId: "run-concurrent",
    status: "completed",
    conclusion: "failure",
  } as const;

  const [first, second] = await Promise.all([
    engine.triggerCiWatchEvent(event),
    engine.triggerCiWatchEvent(event),
  ]);

  const triggered = [...first, ...second];
  expect(triggered).toHaveLength(1);

  await waitForSession(engine, triggered[0]!.session.id, (status) => status === "complete");

  const sessions = engine.listSessions();
  const runs = engine.listWatchRuns(watch.id);
  engine.close();

  expect(sessions).toHaveLength(1);
  expect(runs).toHaveLength(1);
  expect(runs[0]?.eventKey).toBe("run-concurrent");
});

test("engine triggers PR review watches with diff context and return summary", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const pr = await createPrFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return {
        output: [
          "Changed: Adds a README note and a new src.txt file in the PR diff.",
          "Verified: Reviewed the diff stat and patch context from base to head.",
          "Uncertain: No tests were executed in this review-only path.",
          "Next: Decide whether to request changes or approve after manual review.",
        ].join("\n"),
      };
    },
  });

  const watch = engine.createPrWatch({
    repo: pr.repo,
    instructions: "Review the PR and summarize the change.",
    baseBranch: pr.baseBranch,
    policyMode: "observe",
  });

  const triggered = await engine.triggerPrWatchEvent({
    repo: pr.repo,
    prNumber: 12,
    title: "Add feature branch changes",
    baseBranch: pr.baseBranch,
    headBranch: pr.headBranch,
    headSha: pr.headSha,
    action: "synchronize",
    prUrl: "https://example.test/pr/12",
  });

  expect(triggered).toHaveLength(1);
  await waitForSession(engine, triggered[0]!.session.id, (status) => status === "complete");

  const session = engine.getSession(triggered[0]!.session.id);
  const runs = engine.listWatchRuns(watch.id);
  engine.close();

  expect(session?.prompt).toContain(`Base branch: ${pr.baseBranch}`);
  expect(session?.prompt).toContain(`Head branch: ${pr.headBranch}`);
  expect(session?.prompt).toContain("Diff stat:");
  expect(session?.prompt).toContain("Do not push changes.");
  expect(runs).toHaveLength(1);
  expect(runs[0]?.triggerSummary).toContain("PR watch");
  expect(runs[0]?.returnSummary).toContain("Changed: Adds a README note");
  expect(runs[0]?.returnSummary).toContain("Verified: Reviewed the diff stat");
  expect(runs[0]?.returnSummary).toContain("Next: Decide whether to request changes");
});

test("engine ignores duplicate and closed PR events", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-engine-"));
  cleanup.push(dir);
  const pr = await createPrFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "Changed: Reviewed.\nVerified: Diff loaded.\nUncertain: None.\nNext: Review." };
    },
  });

  const watch = engine.createPrWatch({
    repo: pr.repo,
    instructions: "Review the PR and summarize the change.",
    baseBranch: pr.baseBranch,
  });

  const ignored = await engine.triggerPrWatchEvent({
    repo: pr.repo,
    prNumber: 44,
    title: "Closed PR",
    baseBranch: pr.baseBranch,
    headBranch: pr.headBranch,
    action: "closed",
  });
  const first = await engine.triggerPrWatchEvent({
    repo: pr.repo,
    prNumber: 45,
    title: "Open PR",
    baseBranch: pr.baseBranch,
    headBranch: pr.headBranch,
    headSha: pr.headSha,
    action: "opened",
  });
  const duplicate = await engine.triggerPrWatchEvent({
    repo: pr.repo,
    prNumber: 45,
    title: "Open PR",
    baseBranch: pr.baseBranch,
    headBranch: pr.headBranch,
    headSha: pr.headSha,
    action: "synchronize",
  });

  await waitForSession(engine, first[0]!.session.id, (status) => status === "complete");
  const runs = engine.listWatchRuns(watch.id);
  engine.close();

  expect(ignored).toHaveLength(0);
  expect(first).toHaveLength(1);
  expect(duplicate).toHaveLength(0);
  expect(runs).toHaveLength(1);
});
