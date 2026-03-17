import { runCommand } from "./process.ts";
import type { PublishSessionInput, PublishSessionResult, SessionRecord } from "./types.ts";

interface GitHubRepoRef {
  name: string;
  owner: string;
}

interface PullRequestDraft {
  body: string;
  title: string;
}

export function parseGitHubRepoRef(repo: string): GitHubRepoRef | null {
  const httpsMatch = repo.match(/^https:\/\/github\.com\/([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (httpsMatch) {
    return {
      owner: httpsMatch[1]!,
      name: httpsMatch[2]!,
    };
  }

  const sshMatch = repo.match(/^[^@]+@[^:]+:([^/]+)\/([^/]+?)(?:\.git)?$/);
  if (sshMatch) {
    return {
      owner: sshMatch[1]!,
      name: sshMatch[2]!,
    };
  }

  return null;
}

export function buildSessionPrDraft(
  session: SessionRecord,
  input: PublishSessionInput,
): PullRequestDraft {
  const title = input.title?.trim() || defaultTitle(session);
  const body = input.body?.trim() || defaultBody(session);

  return { title, body };
}

export async function publishSession(
  session: SessionRecord,
  input: PublishSessionInput,
): Promise<PublishSessionResult> {
  if (session.status !== "complete") {
    throw new Error(`Session ${session.id} must be complete before publishing`);
  }

  if (!session.workDir || !session.branch) {
    throw new Error(`Session ${session.id} has no workspace branch to publish`);
  }

  const repoRef = parseGitHubRepoRef(session.repo);
  if (!repoRef) {
    throw new Error(`Session ${session.id} repo is not a supported GitHub repo: ${session.repo}`);
  }

  const token = await resolveGitHubToken();
  if (!token) {
    throw new Error(
      "GITHUB_TOKEN, GH_TOKEN, or a logged-in gh CLI session is required to create a pull request",
    );
  }

  await ensurePublishable(session, input.baseBranch);
  await pushBranch(session.workDir, session.branch);

  const draft = buildSessionPrDraft(session, input);
  const pullRequest = await createPullRequest(repoRef, session.branch, input.baseBranch, draft, token);

  return {
    sessionId: session.id,
    branch: session.branch,
    title: draft.title,
    pullRequestNumber: pullRequest.number,
    pullRequestUrl: pullRequest.url,
  };
}

export async function resolveGitHubToken(): Promise<string> {
  const envToken = process.env.GITHUB_TOKEN ?? process.env.GH_TOKEN;
  if (envToken?.trim()) {
    return envToken.trim();
  }

  const ghToken = await runCommand(["gh", "auth", "token"], {
    timeoutMs: 10_000,
  });
  if (ghToken.exitCode === 0 && ghToken.stdout.trim()) {
    return ghToken.stdout.trim();
  }

  return "";
}

function defaultTitle(session: SessionRecord): string {
  const headline = session.outcomeHeadline.trim() || session.resultText.trim().split("\n")[0]?.trim();
  if (headline) {
    return `TeleCoder: ${headline.slice(0, 60)}`;
  }

  return `TeleCoder session ${session.id}`;
}

function defaultBody(session: SessionRecord): string {
  const summary = session.outcomeChanged || session.outcomeHeadline || "No result text captured.";
  const verified = session.outcomeVerified || "Not explicitly verified.";
  const uncertain = session.outcomeUncertain || "No additional uncertainty reported.";
  const next = session.outcomeNext || "Review the session output and decide whether to publish.";

  return [
    "## Summary",
    summary,
    "",
    "## Outcome",
    `- Changed: ${summary}`,
    `- Verified: ${verified}`,
    `- Uncertain: ${uncertain}`,
    `- Next: ${next}`,
    "",
    "## TeleCoder Session",
    `- Session: ${session.id}`,
    `- Branch: ${session.branch}`,
    `- Runtime: acpx -> ${session.runtimeCommand} as ${session.agent}`,
    `- Policy: ${session.policyMode}`,
    `- Workspace writes: ${session.workspaceWritePolicy}`,
  ].join("\n");
}

async function ensurePublishable(session: SessionRecord, baseBranch: string): Promise<void> {
  const statusResult = await runCommand(["git", "-C", session.workDir, "status", "--short"], {
    timeoutMs: 10_000,
  });
  if (statusResult.exitCode !== 0) {
    throw new Error(statusResult.stderr || statusResult.stdout || "git status failed");
  }

  if (statusResult.stdout.trim()) {
    const addResult = await runCommand(["git", "-C", session.workDir, "add", "-A"], {
      timeoutMs: 10_000,
    });
    if (addResult.exitCode !== 0) {
      throw new Error(addResult.stderr || addResult.stdout || "git add failed");
    }

    const commitMessage = defaultTitle(session);
    const commitResult = await runCommand(
      [
        "git",
        "-C",
        session.workDir,
        "-c",
        "user.name=TeleCoder",
        "-c",
        "user.email=telecoder@local",
        "commit",
        "-m",
        commitMessage,
      ],
      { timeoutMs: 10_000 },
    );
    if (commitResult.exitCode !== 0) {
      throw new Error(commitResult.stderr || commitResult.stdout || "git commit failed");
    }
    return;
  }

  const baseRef = await resolveBaseRef(session.workDir, baseBranch);
  const aheadResult = await runCommand(
    ["git", "-C", session.workDir, "rev-list", "--count", `${baseRef}..${session.branch}`],
    {
      timeoutMs: 10_000,
    },
  );
  if (aheadResult.exitCode !== 0) {
    throw new Error(
      aheadResult.stderr || aheadResult.stdout || "git rev-list failed while checking publish state",
    );
  }

  const aheadCount = Number.parseInt(aheadResult.stdout.trim(), 10);
  if (!Number.isFinite(aheadCount) || aheadCount <= 0) {
    throw new Error(`Session ${session.id} has no committed changes to publish`);
  }
}

async function resolveBaseRef(workDir: string, baseBranch: string): Promise<string> {
  const localResult = await runCommand(["git", "-C", workDir, "rev-parse", "--verify", baseBranch], {
    timeoutMs: 10_000,
  });
  if (localResult.exitCode === 0) {
    return baseBranch;
  }

  const remoteRef = `origin/${baseBranch}`;
  const remoteResult = await runCommand(
    ["git", "-C", workDir, "rev-parse", "--verify", remoteRef],
    {
      timeoutMs: 10_000,
    },
  );
  if (remoteResult.exitCode === 0) {
    return remoteRef;
  }

  throw new Error(`Base branch ${baseBranch} is not available in workspace`);
}

async function pushBranch(workDir: string, branch: string): Promise<void> {
  const pushResult = await runCommand(["git", "-C", workDir, "push", "-u", "origin", branch], {
    timeoutMs: 30_000,
  });
  if (pushResult.exitCode !== 0) {
    throw new Error(pushResult.stderr || pushResult.stdout || "git push failed");
  }
}

async function createPullRequest(
  repoRef: GitHubRepoRef,
  headBranch: string,
  baseBranch: string,
  draft: PullRequestDraft,
  token: string,
): Promise<{ number: number; url: string }> {
  const response = await fetch(
    `https://api.github.com/repos/${repoRef.owner}/${repoRef.name}/pulls`,
    {
      method: "POST",
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Bearer ${token}`,
        "Content-Type": "application/json",
        "User-Agent": "telecoder",
      },
      body: JSON.stringify({
        title: draft.title,
        head: headBranch,
        base: baseBranch,
        body: draft.body,
      }),
    },
  );

  const payload = (await response.json()) as
    | { html_url?: string; number?: number; message?: string }
    | undefined;

  if (response.ok && payload?.html_url && typeof payload.number === "number") {
    return {
      number: payload.number,
      url: payload.html_url,
    };
  }

  const existing = await findExistingPullRequest(repoRef, headBranch, token);
  if (existing) {
    return existing;
  }

  throw new Error(
    payload?.message || `GitHub PR creation failed with status ${response.status}`,
  );
}

async function findExistingPullRequest(
  repoRef: GitHubRepoRef,
  headBranch: string,
  token: string,
): Promise<{ number: number; url: string } | null> {
  const response = await fetch(
    `https://api.github.com/repos/${repoRef.owner}/${repoRef.name}/pulls?head=${encodeURIComponent(`${repoRef.owner}:${headBranch}`)}&state=open`,
    {
      headers: {
        Accept: "application/vnd.github+json",
        Authorization: `Bearer ${token}`,
        "User-Agent": "telecoder",
      },
    },
  );

  if (!response.ok) {
    return null;
  }

  const payload = (await response.json()) as Array<{ html_url?: string; number?: number }> | undefined;
  const pullRequest = payload?.[0];
  if (!pullRequest?.html_url || typeof pullRequest.number !== "number") {
    return null;
  }

  return {
    number: pullRequest.number,
    url: pullRequest.html_url,
  };
}
