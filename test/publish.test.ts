import { afterEach, expect, test } from "bun:test";
import { mkdir, mkdtemp, rm, writeFile } from "node:fs/promises";
import { join } from "node:path";
import { tmpdir } from "node:os";

import { parseGitHubRepoRef, publishSession, resolveGitHubToken } from "../src/publish.ts";
import { runCommand } from "../src/process.ts";
import type { SessionRecord } from "../src/types.ts";

const cleanup: string[] = [];

afterEach(async () => {
  delete process.env.GITHUB_TOKEN;
  delete process.env.GH_TOKEN;
  delete process.env.PATH;
  while (cleanup.length > 0) {
    const path = cleanup.pop()!;
    await rm(path, { force: true, recursive: true });
  }
});

async function createPublishFixture(
  dir: string,
  branch = "telecoder/session-test",
): Promise<{
  baseBranch: string;
  remoteDir: string;
  session: SessionRecord;
  workDir: string;
}> {
  const remoteDir = join(dir, "remote.git");
  const workDir = join(dir, "repo");
  await mkdir(workDir, { recursive: true });

  let result = await runCommand(["git", "init", "--bare", remoteDir]);
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git init --bare failed");
  }

  result = await runCommand(["git", "init"], { cwd: workDir });
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git init failed");
  }

  await writeFile(join(workDir, "README.md"), "# publish test\n");
  result = await runCommand(["git", "add", "README.md"], { cwd: workDir });
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git add failed");
  }

  result = await runCommand(
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
    { cwd: workDir },
  );
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git commit failed");
  }

  result = await runCommand(["git", "branch", "--show-current"], { cwd: workDir });
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git branch failed");
  }
  const baseBranch = result.stdout || "main";

  result = await runCommand(["git", "remote", "add", "origin", remoteDir], { cwd: workDir });
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git remote add failed");
  }

  result = await runCommand(["git", "push", "-u", "origin", baseBranch], { cwd: workDir });
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git push main failed");
  }

  result = await runCommand(["git", "checkout", "-b", branch], { cwd: workDir });
  if (result.exitCode !== 0) {
    throw new Error(result.stderr || result.stdout || "git checkout branch failed");
  }

  await writeFile(join(workDir, "README.md"), "# publish test\n\nTeleCoder change\n");

  return {
    baseBranch,
    remoteDir,
    workDir,
    session: {
      id: "sess-1",
      repo: "git@github.com-telecoder-test:jxucoder/telecoder-test-repo.git",
      prompt: "make a doc change",
      agent: "codex",
      parentSessionId: "",
      attempt: 1,
      policyMode: "standard",
      effectivePermissionMode: "approve-all",
      workspaceWritePolicy: "contained",
      maxRuntimeSeconds: 300,
      runtimeCommand: "codex",
      status: "complete",
      ownerId: "owner-1",
      branch,
      workDir,
      resultText:
        "Changed: Adds a README note.\nVerified: README updated.\nUncertain: None.\nNext: Open the PR.",
      outcomeHeadline: "Adds a README note.",
      outcomeChanged: "Adds a README note.",
      outcomeVerified: "README updated.",
      outcomeUncertain: "None.",
      outcomeNext: "Open the PR.",
      error: "",
      failureKind: "",
      claimedAt: "",
      heartbeatAt: "",
      startedAt: "",
      finishedAt: "",
      createdAt: "",
      updatedAt: "",
    },
  };
}

test("parseGitHubRepoRef accepts https and ssh alias formats", () => {
  expect(parseGitHubRepoRef("https://github.com/jxucoder/telecoder-test-repo.git")).toEqual({
    owner: "jxucoder",
    name: "telecoder-test-repo",
  });
  expect(
    parseGitHubRepoRef("git@github.com-telecoder-test:jxucoder/telecoder-test-repo.git"),
  ).toEqual({
    owner: "jxucoder",
    name: "telecoder-test-repo",
  });
  expect(parseGitHubRepoRef("https://github.com/jxucoder/telecoder.test-repo.git")).toEqual({
    owner: "jxucoder",
    name: "telecoder.test-repo",
  });
  expect(parseGitHubRepoRef("git@github.com:jxucoder/telecoder.test-repo.git")).toEqual({
    owner: "jxucoder",
    name: "telecoder.test-repo",
  });
});

test("publishSession commits, pushes, and creates a pull request", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-publish-"));
  cleanup.push(dir);
  const { baseBranch, remoteDir, session } = await createPublishFixture(dir);

  const originalFetch = globalThis.fetch;
  let requestBody = "";
  globalThis.fetch = (async (_url: string | URL | Request, init?: RequestInit) => {
    requestBody = String(init?.body ?? "");
    return new Response(
      JSON.stringify({
        number: 42,
        html_url: "https://github.com/jxucoder/telecoder-test-repo/pull/42",
      }),
      {
        status: 201,
        headers: { "content-type": "application/json" },
      },
    );
  }) as typeof fetch;

  process.env.GITHUB_TOKEN = "test-token";

  try {
    const published = await publishSession(session, { baseBranch });

    expect(published.pullRequestNumber).toBe(42);
    expect(published.pullRequestUrl).toContain("/pull/42");
    expect(requestBody).toContain(`\"base\":\"${baseBranch}\"`);
    expect(requestBody).toContain("\"head\":\"telecoder/session-test\"");

    const pushed = await runCommand(
      ["git", "--git-dir", remoteDir, "rev-parse", "refs/heads/telecoder/session-test"],
      { cwd: dir },
    );
    expect(pushed.exitCode).toBe(0);
    expect(pushed.stdout).not.toBe("");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("publishSession retries after a GitHub API failure once the branch is already committed", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-publish-retry-"));
  cleanup.push(dir);
  const { baseBranch, session } = await createPublishFixture(dir, "telecoder/retry-test");

  const originalFetch = globalThis.fetch;
  let attempts = 0;
  globalThis.fetch = (async () => {
    attempts += 1;
    if (attempts === 1) {
      return new Response(JSON.stringify({ message: "temporary GitHub failure" }), {
        status: 500,
        headers: { "content-type": "application/json" },
      });
    }

    return new Response(
      JSON.stringify({
        number: 43,
        html_url: "https://github.com/jxucoder/telecoder-test-repo/pull/43",
      }),
      {
        status: 201,
        headers: { "content-type": "application/json" },
      },
    );
  }) as typeof fetch;

  process.env.GITHUB_TOKEN = "test-token";

  try {
    await expect(publishSession(session, { baseBranch })).rejects.toThrow(
      "temporary GitHub failure",
    );

    const retried = await publishSession(session, { baseBranch });
    expect(retried.pullRequestNumber).toBe(43);
    expect(attempts).toBe(3);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("publishSession returns the existing pull request on repeated publish", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-publish-existing-"));
  cleanup.push(dir);
  const { baseBranch, session } = await createPublishFixture(dir, "telecoder/existing-pr");

  const originalFetch = globalThis.fetch;
  let postCalls = 0;
  let lookupCalls = 0;
  globalThis.fetch = (async (url: string | URL | Request, init?: RequestInit) => {
    const href = typeof url === "string" ? url : url instanceof URL ? url.toString() : url.url;
    const method = init?.method ?? "GET";

    if (method === "POST") {
      postCalls += 1;
      if (postCalls === 1) {
        return new Response(
          JSON.stringify({
            number: 44,
            html_url: "https://github.com/jxucoder/telecoder-test-repo/pull/44",
          }),
          {
            status: 201,
            headers: { "content-type": "application/json" },
          },
        );
      }

      return new Response(JSON.stringify({ message: "A pull request already exists for foo:bar" }), {
        status: 422,
        headers: { "content-type": "application/json" },
      });
    }

    if (href.includes("/pulls?head=")) {
      lookupCalls += 1;
      return new Response(
        JSON.stringify([
          {
            number: 44,
            html_url: "https://github.com/jxucoder/telecoder-test-repo/pull/44",
          },
        ]),
        {
          status: 200,
          headers: { "content-type": "application/json" },
        },
      );
    }

    throw new Error(`Unexpected fetch call: ${method} ${href}`);
  }) as typeof fetch;

  process.env.GITHUB_TOKEN = "test-token";

  try {
    const first = await publishSession(session, { baseBranch });
    const second = await publishSession(session, { baseBranch });

    expect(first.pullRequestNumber).toBe(44);
    expect(second.pullRequestNumber).toBe(44);
    expect(postCalls).toBe(2);
    expect(lookupCalls).toBe(1);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("resolveGitHubToken falls back to gh auth token", async () => {
  const dir = await mkdtemp(join(tmpdir(), "telecoder-gh-token-"));
  cleanup.push(dir);

  const binDir = join(dir, "bin");
  await mkdir(binDir, { recursive: true });
  const ghPath = join(binDir, "gh");
  await writeFile(
    ghPath,
    "#!/bin/sh\nif [ \"$1\" = \"auth\" ] && [ \"$2\" = \"token\" ]; then\n  printf 'gh-test-token\\n'\n  exit 0\nfi\nexit 1\n",
  );

  const chmodResult = await runCommand(["chmod", "+x", ghPath]);
  if (chmodResult.exitCode !== 0) {
    throw new Error(chmodResult.stderr || chmodResult.stdout || "chmod failed");
  }

  process.env.PATH = `${binDir}:${process.env.PATH ?? ""}`;
  expect(await resolveGitHubToken()).toBe("gh-test-token");
});
