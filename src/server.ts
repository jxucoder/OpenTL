import { TeleCoderEngine } from "./engine.ts";
import { parsePolicyMode } from "./policy.ts";
import { parseWatchKind, parseWatchStatus } from "./watch.ts";
import type { SessionEvent, SessionListQuery, SessionStatusFilter, WatchListQuery } from "./types.ts";

function json(data: unknown, status = 200): Response {
  return new Response(JSON.stringify(data, null, 2), {
    status,
    headers: {
      "content-type": "application/json; charset=utf-8",
    },
  });
}

function sseChunk(event: SessionEvent): string {
  const lines = event.data.split("\n").map((line) => `data: ${line}`);
  return [`id: ${event.id}`, `event: ${event.type}`, ...lines, "", ""].join("\n");
}

function parseSessionStatusFilter(raw: string | null): SessionStatusFilter | undefined {
  if (!raw) {
    return undefined;
  }

  switch (raw) {
    case "pending":
    case "running":
    case "complete":
    case "error":
    case "active":
      return raw;
    default:
      throw new Error(`Invalid session status filter: ${raw}`);
  }
}

function parseLimit(raw: string | null): number | undefined {
  if (!raw) {
    return undefined;
  }

  const limit = Number.parseInt(raw, 10);
  if (!Number.isFinite(limit) || limit <= 0) {
    throw new Error(`Invalid limit: ${raw}`);
  }

  return limit;
}

function parseSessionListQuery(url: URL): SessionListQuery {
  return {
    status: parseSessionStatusFilter(url.searchParams.get("status")),
    agent: url.searchParams.get("agent") ?? undefined,
    parentSessionId: url.searchParams.get("parent") ?? undefined,
    lineageSessionId: url.searchParams.get("lineage") ?? undefined,
    policyMode: parsePolicyMode(url.searchParams.get("policy") ?? undefined),
  };
}

function parseWatchListQuery(url: URL): WatchListQuery {
  return {
    kind: parseWatchKind(url.searchParams.get("kind") ?? undefined),
    repo: url.searchParams.get("repo") ?? undefined,
    status: parseWatchStatus(url.searchParams.get("status") ?? undefined),
  };
}

export function startServer(
  engine: TeleCoderEngine,
  listenHost: string,
  listenPort: number,
): void {
  const server = Bun.serve({
    hostname: listenHost,
    port: listenPort,
    fetch(request) {
      return handleRequest(engine, request);
    },
  });

  console.log(`TeleCoder listening on http://${listenHost}:${server.port}`);
}

export async function handleRequest(
  engine: TeleCoderEngine,
  request: Request,
): Promise<Response> {
  const url = new URL(request.url);
  const path = url.pathname;

  if (request.method === "GET" && path === "/health") {
    return json({ status: "ok" });
  }

  if (request.method === "GET" && path === "/api/sessions") {
    try {
      return json(engine.listSessions(parseSessionListQuery(url)));
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  if (request.method === "GET" && path === "/api/inbox") {
    try {
      return json(engine.listInbox(parseLimit(url.searchParams.get("limit"))));
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  if (request.method === "GET" && path === "/api/watches") {
    try {
      return json(engine.listWatches(parseWatchListQuery(url)));
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  if (request.method === "POST" && path === "/api/watches") {
    const body = (await request.json()) as {
      agent?: string;
      branch?: string;
      base?: string;
      head?: string;
      instructions?: string;
      kind?: string;
      policy?: string;
      repo?: string;
      workflow?: string;
    };

    if (!body.repo || !body.instructions) {
      return json({ error: "repo and instructions are required" }, 400);
    }

    try {
      const kind = parseWatchKind(body.kind) ?? "ci_failure";
      if (kind === "ci_failure") {
        const watch = engine.createCiWatch({
          repo: body.repo,
          instructions: body.instructions,
          agent: body.agent,
          policyMode: parsePolicyMode(body.policy),
          workflowName: body.workflow,
          branchName: body.branch,
        });
        return json(watch, 201);
      }

      if (kind === "pr_review") {
        const watch = engine.createPrWatch({
          repo: body.repo,
          instructions: body.instructions,
          agent: body.agent,
          policyMode: parsePolicyMode(body.policy),
          baseBranch: body.base,
          headBranch: body.head,
        });
        return json(watch, 201);
      }

      return json({ error: `Unsupported watch kind: ${kind}` }, 400);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  if (request.method === "POST" && path === "/api/sessions") {
    const body = (await request.json()) as {
      agent?: string;
      policy?: string;
      prompt?: string;
      repo?: string;
    };
    if (!body.repo || !body.prompt) {
      return json({ error: "repo and prompt are required" }, 400);
    }
    try {
      const session = await engine.createTask({
        repo: body.repo,
        prompt: body.prompt,
        agent: body.agent,
        policyMode: parsePolicyMode(body.policy),
      });
      return json(session, 201);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  const sessionMatch = path.match(/^\/api\/sessions\/([^/]+)$/);
  if (request.method === "GET" && sessionMatch) {
    const session = engine.getSession(sessionMatch[1]!);
    if (!session) {
      return json({ error: "session not found" }, 404);
    }
    return json(session);
  }

  const publishMatch = path.match(/^\/api\/sessions\/([^/]+)\/publish$/);
  if (request.method === "POST" && publishMatch) {
    const body = (await request.json()) as {
      base?: string;
      body?: string;
      title?: string;
    };

    if (!body.base) {
      return json({ error: "base is required" }, 400);
    }

    try {
      const result = await engine.publishSession(publishMatch[1]!, {
        baseBranch: body.base,
        title: body.title,
        body: body.body,
      });
      return json(result, 201);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("not found")) {
        return json({ error: message }, 404);
      }
      return json({ error: message }, 400);
    }
  }

  const watchMatch = path.match(/^\/api\/watches\/([^/]+)$/);
  if (request.method === "GET" && watchMatch) {
    const watch = engine.getWatch(watchMatch[1]!);
    if (!watch) {
      return json({ error: "watch not found" }, 404);
    }
    return json(watch);
  }

  const watchRunsMatch = path.match(/^\/api\/watches\/([^/]+)\/runs$/);
  if (request.method === "GET" && watchRunsMatch) {
    const watch = engine.getWatch(watchRunsMatch[1]!);
    if (!watch) {
      return json({ error: "watch not found" }, 404);
    }
    return json(engine.listWatchRuns(watch.id));
  }

  if (request.method === "POST" && path === "/api/watch-events/ci") {
    const body = (await request.json()) as {
      branch?: string;
      conclusion?: string;
      repo?: string;
      runId?: string;
      runUrl?: string;
      sha?: string;
      status?: string;
      summary?: string;
      workflow?: string;
    };

    if (!body.repo || !body.workflow || !body.branch || !body.runId) {
      return json({ error: "repo, workflow, branch, and runId are required" }, 400);
    }

    try {
      const triggered = await engine.triggerCiWatchEvent({
        repo: body.repo,
        workflowName: body.workflow,
        branchName: body.branch,
        runId: body.runId,
        runUrl: body.runUrl,
        sha: body.sha,
        status: body.status,
        conclusion: body.conclusion,
        summary: body.summary,
      });

      return json(
        triggered.map((item) => ({
          watchId: item.watch.id,
          sessionId: item.session.id,
          runId: item.run.id,
          triggerSummary: item.run.triggerSummary,
        })),
        201,
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  if (request.method === "POST" && path === "/api/watch-events/pr") {
    const body = (await request.json()) as {
      action?: string;
      base?: string;
      body?: string;
      diffText?: string;
      head?: string;
      headSha?: string;
      prNumber?: number;
      prUrl?: string;
      repo?: string;
      title?: string;
    };

    if (!body.repo || !body.title || !body.base || !body.head || !body.prNumber) {
      return json({ error: "repo, prNumber, title, base, and head are required" }, 400);
    }

    try {
      const triggered = await engine.triggerPrWatchEvent({
        repo: body.repo,
        prNumber: body.prNumber,
        title: body.title,
        baseBranch: body.base,
        headBranch: body.head,
        action: body.action,
        prUrl: body.prUrl,
        headSha: body.headSha,
        body: body.body,
        diffText: body.diffText,
      });

      return json(
        triggered.map((item) => ({
          watchId: item.watch.id,
          sessionId: item.session.id,
          runId: item.run.id,
          triggerSummary: item.run.triggerSummary,
        })),
        201,
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  const lineageMatch = path.match(/^\/api\/sessions\/([^/]+)\/lineage$/);
  if (request.method === "GET" && lineageMatch) {
    const sessionId = lineageMatch[1]!;
    const session = engine.getSession(sessionId);
    if (!session) {
      return json({ error: "session not found" }, 404);
    }

    try {
      return json(
        engine.listSessions({
          ...parseSessionListQuery(url),
          lineageSessionId: sessionId,
        }),
      );
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      return json({ error: message }, 400);
    }
  }

  const rerunMatch = path.match(/^\/api\/sessions\/([^/]+)\/rerun$/);
  if (request.method === "POST" && rerunMatch) {
    try {
      const session = await engine.rerunSession(rerunMatch[1]!);
      return json(session, 201);
    } catch (error) {
      const message = error instanceof Error ? error.message : String(error);
      if (message.includes("not found")) {
        return json({ error: message }, 404);
      }
      if (message.includes("still active")) {
        return json({ error: message }, 409);
      }
      return json({ error: message }, 400);
    }
  }

  const eventsMatch = path.match(/^\/api\/sessions\/([^/]+)\/events$/);
  if (request.method === "GET" && eventsMatch) {
    const sessionId = eventsMatch[1]!;
    const session = engine.getSession(sessionId);
    if (!session) {
      return json({ error: "session not found" }, 404);
    }

    const afterId = Number.parseInt(url.searchParams.get("after") ?? "0", 10) || 0;
    const wantsSse = request.headers.get("accept")?.includes("text/event-stream") ?? false;
    if (!wantsSse) {
      return json(engine.listEvents(sessionId, afterId));
    }

    const encoder = new TextEncoder();
    return new Response(
      new ReadableStream({
        start(controller) {
          let closed = false;
          let unsubscribe = () => {};

          const close = () => {
            if (closed) {
              return;
            }
            closed = true;
            unsubscribe();
            controller.close();
          };

          const push = (event: SessionEvent) => {
            if (closed) {
              return;
            }
            controller.enqueue(encoder.encode(sseChunk(event)));
            if (event.type === "done" || event.type === "error") {
              close();
            }
          };

          for (const event of engine.listEvents(sessionId, afterId)) {
            push(event);
          }

          if (closed) {
            return;
          }

          unsubscribe = engine.subscribe(sessionId, push);
          request.signal.addEventListener("abort", close, { once: true });
        },
      }),
      {
        headers: {
          "cache-control": "no-cache",
          connection: "keep-alive",
          "content-type": "text/event-stream; charset=utf-8",
        },
      },
    );
  }

  return json({ error: "not found" }, 404);
}
