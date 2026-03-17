import { afterEach, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { TeleCoderEngine } from "../src/engine.ts";
import { runCommand } from "../src/process.ts";
import { handleRequest } from "../src/server.ts";
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
  await writeFile(join(repoDir, "README.md"), "# server test\n");

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

  await writeFile(join(repo, "README.md"), "# server test\n\nPR change\n");
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

async function waitForStatus(
  engine: TeleCoderEngine,
  sessionId: string,
  expectedStatus: string,
): Promise<void> {
  for (let attempt = 0; attempt < 100; attempt += 1) {
    if (engine.getSession(sessionId)?.status === expectedStatus) {
      return;
    }
    await Bun.sleep(20);
  }

  throw new Error(`Timed out waiting for session ${sessionId} to become ${expectedStatus}`);
}

test("POST /api/sessions/:id/rerun creates a linked rerun session", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "complete-session",
    repo,
    prompt: "hello",
    agent: "codex",
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

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/sessions/complete-session/rerun", { method: "POST" }),
  );
  const body = (await response.json()) as { attempt: number; id: string; parentSessionId: string };

  await waitForStatus(engine, body.id, "complete");
  const rerun = engine.getSession(body.id);
  engine.close();

  expect(response.status).toBe(201);
  expect(body.parentSessionId).toBe("complete-session");
  expect(body.attempt).toBe(2);
  expect(rerun?.status).toBe("complete");
  expect(rerun?.resultText).toBe("rerun output");
});

test("POST /api/sessions/:id/rerun rejects active sessions", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "running-session",
    repo: "git@example.com/repo.git",
    prompt: "hello",
    agent: "codex",
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

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/sessions/running-session/rerun", { method: "POST" }),
  );
  const body = (await response.json()) as { error: string };
  engine.close();

  expect(response.status).toBe(409);
  expect(body.error).toContain("still active");
});

test("GET /api/sessions supports status, parent, and policy filters", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
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
    id: "child",
    repo: "git@example.com/repo.git",
    prompt: "child",
    agent: "claude",
    parentSessionId: "root",
    status: "running",
    attempt: 2,
    policyMode: "locked",
    effectivePermissionMode: "deny-all",
    maxRuntimeSeconds: 60,
  });
  store.createSession({
    id: "other",
    repo: "git@example.com/repo.git",
    prompt: "other",
    agent: "claude",
    status: "error",
    attempt: 1,
    policyMode: "observe",
  });

  const engine = new TeleCoderEngine(makeConfig(dir), store, {
    async runTask() {
      return { output: "" };
    },
  });

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/sessions?status=active&parent=root&policy=locked"),
  );
  const body = (await response.json()) as Array<{ id: string }>;
  engine.close();

  expect(response.status).toBe(200);
  expect(body.map((session) => session.id)).toEqual(["child"]);
});

test("GET /api/inbox returns structured session outcomes", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "complete",
    repo: "git@example.com/repo.git",
    prompt: "complete",
    agent: "codex",
    status: "complete",
    resultText: "Changed: Fixed auth test.\nVerified: Ran targeted tests.\nNext: Watch CI.",
  });
  store.createSession({
    id: "error",
    repo: "git@example.com/repo.git",
    prompt: "error",
    agent: "codex",
    status: "error",
    error: "permission denied by runtime",
  });

  const engine = new TeleCoderEngine(makeConfig(dir), store, {
    async runTask() {
      return { output: "" };
    },
  });

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/inbox?limit=2"),
  );
  const body = (await response.json()) as Array<{
    id: string;
    outcomeChanged: string;
    outcomeHeadline: string;
    outcomeVerified: string;
  }>;
  engine.close();

  expect(response.status).toBe(200);
  expect(body).toHaveLength(2);
  expect(body.some((session) => session.id === "complete")).toBe(true);
  expect(body.find((session) => session.id === "complete")?.outcomeChanged).toBe("Fixed auth test.");
  expect(body.find((session) => session.id === "complete")?.outcomeVerified).toBe(
    "Ran targeted tests.",
  );
  expect(body.find((session) => session.id === "error")?.outcomeHeadline).toContain("Failed:");
});

test("POST /api/sessions accepts explicit policy mode", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "policy output" };
    },
  });

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/sessions", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        repo,
        prompt: "hello",
        policy: "standard",
      }),
    }),
  );
  const body = (await response.json()) as { id: string; policyMode: string };
  await waitForStatus(engine, body.id, "complete");
  const session = engine.getSession(body.id);
  engine.close();

  expect(response.status).toBe(201);
  expect(body.policyMode).toBe("standard");
  expect(session?.effectivePermissionMode).toBe("approve-all");
});

test("GET /api/sessions/:id/lineage returns the linked family", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);

  const store = new TeleCoderStore(join(dir, "telecoder.sqlite"));
  store.createSession({
    id: "root",
    repo: "git@example.com/repo.git",
    prompt: "root",
    agent: "codex",
    status: "complete",
    attempt: 1,
  });
  store.createSession({
    id: "child",
    repo: "git@example.com/repo.git",
    prompt: "child",
    agent: "codex",
    parentSessionId: "root",
    status: "complete",
    attempt: 2,
  });
  store.createSession({
    id: "grandchild",
    repo: "git@example.com/repo.git",
    prompt: "grandchild",
    agent: "claude",
    parentSessionId: "child",
    status: "complete",
    attempt: 3,
  });
  store.createSession({
    id: "sibling",
    repo: "git@example.com/repo.git",
    prompt: "sibling",
    agent: "claude",
    parentSessionId: "root",
    status: "error",
    attempt: 2,
  });

  const engine = new TeleCoderEngine(makeConfig(dir), store, {
    async runTask() {
      return { output: "" };
    },
  });

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/sessions/grandchild/lineage?status=complete"),
  );
  const body = (await response.json()) as Array<{ id: string }>;
  engine.close();

  expect(response.status).toBe(200);
  expect(body.map((session) => session.id)).toEqual(["root", "child", "grandchild"]);
});

test("GET /api/sessions rejects invalid status filters", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "" };
    },
  });

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/sessions?status=broken"),
  );
  const body = (await response.json()) as { error: string };
  engine.close();

  expect(response.status).toBe(400);
  expect(body.error).toContain("Invalid session status filter");
});

test("POST /api/sessions rejects invalid policy modes", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "" };
    },
  });

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/sessions", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        repo,
        prompt: "hello",
        policy: "unsafe",
      }),
    }),
  );
  const body = (await response.json()) as { error: string };
  engine.close();

  expect(response.status).toBe(400);
  expect(body.error).toContain("Invalid TELECODER_POLICY_MODE");
});

test("POST /api/watches creates a CI watch and GET /api/watches lists it", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "" };
    },
  });

  const createResponse = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/watches", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        repo: "git@example.com/repo.git",
        instructions: "Investigate the failing build",
        workflow: "build",
        branch: "main",
        policy: "observe",
      }),
    }),
  );
  const created = (await createResponse.json()) as { id: string; workflowName: string };

  const listResponse = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/watches?kind=ci_failure&status=active"),
  );
  const listed = (await listResponse.json()) as Array<{ id: string }>;
  engine.close();

  expect(createResponse.status).toBe(201);
  expect(created.workflowName).toBe("build");
  expect(listResponse.status).toBe(200);
  expect(listed.map((watch) => watch.id)).toEqual([created.id]);
});

test("POST /api/watch-events/ci triggers matching watch and stores run", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "Likely flaky test in build pipeline" };
    },
  });
  const watch = engine.createCiWatch({
    repo,
    instructions: "Investigate the failing build.",
    workflowName: "build",
    branchName: "main",
  });

  const triggerResponse = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/watch-events/ci", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        repo,
        workflow: "build",
        branch: "main",
        runId: "run-123",
        runUrl: "https://ci.example/run/123",
        status: "completed",
        conclusion: "failure",
      }),
    }),
  );
  const triggered = (await triggerResponse.json()) as Array<{ sessionId: string }>;
  await waitForStatus(engine, triggered[0]!.sessionId, "complete");

  const runsResponse = await handleRequest(
    engine,
    new Request(`http://telecoder.test/api/watches/${watch.id}/runs`),
  );
  const runs = (await runsResponse.json()) as Array<{ resultSummary: string }>;
  engine.close();

  expect(triggerResponse.status).toBe(201);
  expect(triggered).toHaveLength(1);
  expect(runsResponse.status).toBe(200);
  expect(runs[0]?.resultSummary).toContain("Likely flaky test");
});

test("POST /api/watch-events/ci ignores non-failure conclusions", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);
  const repo = await createRepoFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "" };
    },
  });
  engine.createCiWatch({
    repo,
    instructions: "Investigate the failing build.",
    workflowName: "build",
  });

  const triggerResponse = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/watch-events/ci", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        repo,
        workflow: "build",
        branch: "main",
        runId: "run-ok",
        status: "completed",
        conclusion: "success",
      }),
    }),
  );
  const triggered = (await triggerResponse.json()) as Array<unknown>;
  engine.close();

  expect(triggerResponse.status).toBe(201);
  expect(triggered).toHaveLength(0);
});

test("POST /api/watches creates a PR review watch", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return { output: "" };
    },
  });

  const response = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/watches", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        kind: "pr_review",
        repo: "git@example.com/repo.git",
        instructions: "Review the PR diff.",
        base: "main",
      }),
    }),
  );
  const body = (await response.json()) as { baseBranch: string; kind: string };
  engine.close();

  expect(response.status).toBe(201);
  expect(body.kind).toBe("pr_review");
  expect(body.baseBranch).toBe("main");
});

test("POST /api/watch-events/pr triggers matching PR review watch and stores return summary", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-server-"));
  cleanup.push(dir);
  const pr = await createPrFixture(dir);

  const engine = new TeleCoderEngine(makeConfig(dir), undefined, {
    async runTask() {
      return {
        output: [
          "Changed: Reviews the README and src.txt changes in the PR.",
          "Verified: Loaded diff stat and patch context.",
          "Uncertain: Tests were not run.",
          "Next: Review manually before deciding on approval.",
        ].join("\n"),
      };
    },
  });
  const watch = engine.createPrWatch({
    repo: pr.repo,
    instructions: "Review the PR diff.",
    baseBranch: pr.baseBranch,
  });

  const triggerResponse = await handleRequest(
    engine,
    new Request("http://telecoder.test/api/watch-events/pr", {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({
        repo: pr.repo,
        prNumber: 18,
        title: "Review this PR",
        base: pr.baseBranch,
        head: pr.headBranch,
        headSha: pr.headSha,
        action: "synchronize",
        prUrl: "https://example.test/pr/18",
      }),
    }),
  );
  const triggered = (await triggerResponse.json()) as Array<{ sessionId: string }>;
  await waitForStatus(engine, triggered[0]!.sessionId, "complete");

  const runsResponse = await handleRequest(
    engine,
    new Request(`http://telecoder.test/api/watches/${watch.id}/runs`),
  );
  const runs = (await runsResponse.json()) as Array<{ returnSummary: string }>;
  engine.close();

  expect(triggerResponse.status).toBe(201);
  expect(triggered).toHaveLength(1);
  expect(runsResponse.status).toBe(200);
  expect(runs[0]?.returnSummary).toContain("PR watch");
  expect(runs[0]?.returnSummary).toContain("Changed: Reviews the README");
  expect(runs[0]?.returnSummary).toContain("Next: Review manually");
});
