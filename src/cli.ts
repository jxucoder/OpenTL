import { loadConfig } from "./config.ts";
import { TeleCoderEngine } from "./engine.ts";
import { parsePolicyMode } from "./policy.ts";
import { startServer } from "./server.ts";
import { parseWatchKind, parseWatchStatus } from "./watch.ts";
import type {
  SessionListQuery,
  SessionStatusFilter,
  TeleCoderPolicyMode,
  WatchListQuery,
} from "./types.ts";

interface ParsedArgs {
  flags: Map<string, string | boolean>;
  positionals: string[];
}

function parseArgs(argv: string[]): ParsedArgs {
  const flags = new Map<string, string | boolean>();
  const positionals: string[] = [];

  for (let index = 0; index < argv.length; index += 1) {
    const token = argv[index]!;
    if (!token.startsWith("--")) {
      positionals.push(token);
      continue;
    }

    const key = token.slice(2);
    const next = argv[index + 1];
    if (!next || next.startsWith("--")) {
      flags.set(key, true);
      continue;
    }
    flags.set(key, next);
    index += 1;
  }

  return { flags, positionals };
}

function usage(): string {
  return `
TeleCoder

Commands:
  bun src/cli.ts serve
  bun src/cli.ts run --repo <repo> [--agent <agent>] [--policy <locked|observe|standard>] <prompt>
  bun src/cli.ts watch-add-ci --repo <repo> [--workflow <name>] [--branch <name>] [--agent <agent>] [--policy <locked|observe|standard>] <instructions>
  bun src/cli.ts watch-add-pr --repo <repo> [--base <branch>] [--head <branch>] [--agent <agent>] [--policy <locked|observe|standard>] <instructions>
  bun src/cli.ts watch-list [--kind <kind>] [--status <status>] [--repo <repo>]
  bun src/cli.ts watch-runs <watch-id>
  bun src/cli.ts trigger-ci --repo <repo> --workflow <name> --branch <name> --run-id <id> [--run-url <url>] [--sha <sha>] [--status <status>] [--conclusion <conclusion>] [--summary <text>]
  bun src/cli.ts trigger-pr --repo <repo> --pr-number <n> --title <title> --base <branch> --head <branch> [--action <action>] [--url <url>] [--head-sha <sha>] [--body <text>] [--diff-text <text>]
  bun src/cli.ts publish-session <session-id> --base <branch> [--title <title>] [--body <body>]
  bun src/cli.ts rerun <session-id>
  bun src/cli.ts status <session-id>
  bun src/cli.ts events <session-id>
  bun src/cli.ts lineage <session-id>
  bun src/cli.ts list [--status <status|active>] [--agent <agent>] [--policy <locked|observe|standard>] [--parent <session-id>] [--lineage <session-id>]
  bun src/cli.ts inbox [--limit <n>]
  bun src/cli.ts config
`.trim();
}

function parseSessionStatusFilter(raw: string | boolean | undefined): SessionStatusFilter | undefined {
  if (typeof raw !== "string" || raw.trim() === "") {
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

function parseSessionListQuery(flags: Map<string, string | boolean>): SessionListQuery {
  const status = parseSessionStatusFilter(flags.get("status"));
  const agent = flags.get("agent");
  const policy = parsePolicyFlag(flags.get("policy"));
  const parent = flags.get("parent");
  const lineage = flags.get("lineage");

  return {
    status,
    agent: typeof agent === "string" ? agent : undefined,
    policyMode: policy,
    parentSessionId: typeof parent === "string" ? parent : undefined,
    lineageSessionId: typeof lineage === "string" ? lineage : undefined,
  };
}

function parseWatchListQuery(flags: Map<string, string | boolean>): WatchListQuery {
  const kind = flags.get("kind");
  const status = flags.get("status");
  const repo = flags.get("repo");

  return {
    kind: parseWatchKind(typeof kind === "string" ? kind : undefined),
    status: parseWatchStatus(typeof status === "string" ? status : undefined),
    repo: typeof repo === "string" ? repo : undefined,
  };
}

function parsePolicyFlag(raw: string | boolean | undefined): TeleCoderPolicyMode | undefined {
  if (typeof raw !== "string" || raw.trim() === "") {
    return undefined;
  }

  return parsePolicyMode(raw);
}

function parseLimitFlag(raw: string | boolean | undefined): number | undefined {
  if (typeof raw !== "string" || raw.trim() === "") {
    return undefined;
  }

  const limit = Number.parseInt(raw, 10);
  if (!Number.isFinite(limit) || limit <= 0) {
    throw new Error(`Invalid --limit: ${raw}`);
  }

  return limit;
}

function printEvent(type: string, data: string): void {
  if (type === "output") {
    console.log(data);
    return;
  }
  console.log(`[${type}] ${data}`);
}

async function followSession(engine: TeleCoderEngine, sessionId: string): Promise<boolean> {
  const seen = new Set<number>();

  return await new Promise<boolean>((resolve) => {
    const handle = (event: { data: string; id: number; type: string }) => {
      if (seen.has(event.id)) {
        return;
      }
      seen.add(event.id);
      printEvent(event.type, event.data);

      if (event.type === "done") {
        unsubscribe();
        resolve(true);
      }
      if (event.type === "error") {
        unsubscribe();
        resolve(false);
      }
    };

    const unsubscribe = engine.subscribe(sessionId, handle);
    for (const event of engine.listEvents(sessionId)) {
      handle(event);
    }
  });
}

async function main(): Promise<void> {
  const [command, ...rest] = Bun.argv.slice(2);
  if (!command || command === "help" || command === "--help") {
    console.log(usage());
    return;
  }

  const config = await loadConfig();
  const engine = new TeleCoderEngine(config);

  try {
    switch (command) {
      case "serve": {
        startServer(engine, config.listenHost, config.listenPort);
        await new Promise(() => {});
        return;
      }
      case "run": {
        const parsed = parseArgs(rest);
        const repo = parsed.flags.get("repo");
        const agent = parsed.flags.get("agent");
        const policy = parsePolicyFlag(parsed.flags.get("policy"));
        const prompt = parsed.positionals.join(" ").trim();

        if (typeof repo !== "string" || !prompt) {
          throw new Error("run requires --repo <repo> and a prompt");
        }

        const session = await engine.createTask({
          repo,
          prompt,
          agent: typeof agent === "string" ? agent : undefined,
          policyMode: policy,
        });

        console.log(`Session ${session.id} created`);
        const ok = await followSession(engine, session.id);
        process.exitCode = ok ? 0 : 1;
        return;
      }
      case "watch-add-ci": {
        const parsed = parseArgs(rest);
        const repo = parsed.flags.get("repo");
        const workflow = parsed.flags.get("workflow");
        const branch = parsed.flags.get("branch");
        const agent = parsed.flags.get("agent");
        const policy = parsePolicyFlag(parsed.flags.get("policy"));
        const instructions = parsed.positionals.join(" ").trim();

        if (typeof repo !== "string" || !instructions) {
          throw new Error("watch-add-ci requires --repo <repo> and instructions");
        }

        const watch = engine.createCiWatch({
          repo,
          instructions,
          workflowName: typeof workflow === "string" ? workflow : undefined,
          branchName: typeof branch === "string" ? branch : undefined,
          agent: typeof agent === "string" ? agent : undefined,
          policyMode: policy,
        });
        console.log(JSON.stringify(watch, null, 2));
        return;
      }
      case "watch-add-pr": {
        const parsed = parseArgs(rest);
        const repo = parsed.flags.get("repo");
        const base = parsed.flags.get("base");
        const head = parsed.flags.get("head");
        const agent = parsed.flags.get("agent");
        const policy = parsePolicyFlag(parsed.flags.get("policy"));
        const instructions = parsed.positionals.join(" ").trim();

        if (typeof repo !== "string" || !instructions) {
          throw new Error("watch-add-pr requires --repo <repo> and instructions");
        }

        const watch = engine.createPrWatch({
          repo,
          instructions,
          baseBranch: typeof base === "string" ? base : undefined,
          headBranch: typeof head === "string" ? head : undefined,
          agent: typeof agent === "string" ? agent : undefined,
          policyMode: policy,
        });
        console.log(JSON.stringify(watch, null, 2));
        return;
      }
      case "watch-list": {
        const parsed = parseArgs(rest);
        console.log(JSON.stringify(engine.listWatches(parseWatchListQuery(parsed.flags)), null, 2));
        return;
      }
      case "watch-runs": {
        const watchId = rest[0];
        if (!watchId) {
          throw new Error("watch-runs requires a watch id");
        }
        const watch = engine.getWatch(watchId);
        if (!watch) {
          throw new Error(`Watch ${watchId} not found`);
        }
        console.log(JSON.stringify(engine.listWatchRuns(watchId), null, 2));
        return;
      }
      case "trigger-ci": {
        const parsed = parseArgs(rest);
        const repo = parsed.flags.get("repo");
        const workflow = parsed.flags.get("workflow");
        const branch = parsed.flags.get("branch");
        const runId = parsed.flags.get("run-id");
        const runUrl = parsed.flags.get("run-url");
        const sha = parsed.flags.get("sha");
        const status = parsed.flags.get("status");
        const conclusion = parsed.flags.get("conclusion");
        const summary = parsed.flags.get("summary");

        if (
          typeof repo !== "string" ||
          typeof workflow !== "string" ||
          typeof branch !== "string" ||
          typeof runId !== "string"
        ) {
          throw new Error(
            "trigger-ci requires --repo, --workflow, --branch, and --run-id",
          );
        }

        const triggered = await engine.triggerCiWatchEvent({
          repo,
          workflowName: workflow,
          branchName: branch,
          runId,
          runUrl: typeof runUrl === "string" ? runUrl : undefined,
          sha: typeof sha === "string" ? sha : undefined,
          status: typeof status === "string" ? status : undefined,
          conclusion: typeof conclusion === "string" ? conclusion : undefined,
          summary: typeof summary === "string" ? summary : undefined,
        });
        console.log(JSON.stringify(triggered, null, 2));
        let ok = true;
        for (const item of triggered) {
          ok = (await followSession(engine, item.session.id)) && ok;
        }
        process.exitCode = ok ? 0 : 1;
        return;
      }
      case "trigger-pr": {
        const parsed = parseArgs(rest);
        const repo = parsed.flags.get("repo");
        const prNumber = parsed.flags.get("pr-number");
        const title = parsed.flags.get("title");
        const base = parsed.flags.get("base");
        const head = parsed.flags.get("head");
        const action = parsed.flags.get("action");
        const url = parsed.flags.get("url");
        const headSha = parsed.flags.get("head-sha");
        const body = parsed.flags.get("body");
        const diffText = parsed.flags.get("diff-text");

        if (
          typeof repo !== "string" ||
          typeof prNumber !== "string" ||
          typeof title !== "string" ||
          typeof base !== "string" ||
          typeof head !== "string"
        ) {
          throw new Error(
            "trigger-pr requires --repo, --pr-number, --title, --base, and --head",
          );
        }

        const prNumberValue = Number.parseInt(prNumber, 10);
        if (!Number.isFinite(prNumberValue) || prNumberValue <= 0) {
          throw new Error(`Invalid --pr-number: ${prNumber}`);
        }

        const triggered = await engine.triggerPrWatchEvent({
          repo,
          prNumber: prNumberValue,
          title,
          baseBranch: base,
          headBranch: head,
          action: typeof action === "string" ? action : undefined,
          prUrl: typeof url === "string" ? url : undefined,
          headSha: typeof headSha === "string" ? headSha : undefined,
          body: typeof body === "string" ? body : undefined,
          diffText: typeof diffText === "string" ? diffText : undefined,
        });
        console.log(JSON.stringify(triggered, null, 2));
        let ok = true;
        for (const item of triggered) {
          ok = (await followSession(engine, item.session.id)) && ok;
        }
        process.exitCode = ok ? 0 : 1;
        return;
      }
      case "publish-session": {
        const sessionId = rest[0];
        const parsed = parseArgs(rest.slice(1));
        const base = parsed.flags.get("base");
        const title = parsed.flags.get("title");
        const body = parsed.flags.get("body");

        if (!sessionId) {
          throw new Error("publish-session requires a session id");
        }
        if (typeof base !== "string") {
          throw new Error("publish-session requires --base <branch>");
        }

        const result = await engine.publishSession(sessionId, {
          baseBranch: base,
          title: typeof title === "string" ? title : undefined,
          body: typeof body === "string" ? body : undefined,
        });
        console.log(JSON.stringify(result, null, 2));
        return;
      }
      case "status": {
        const sessionId = rest[0];
        if (!sessionId) {
          throw new Error("status requires a session id");
        }
        const session = engine.getSession(sessionId);
        if (!session) {
          throw new Error(`Session ${sessionId} not found`);
        }
        console.log(JSON.stringify(session, null, 2));
        return;
      }
      case "events": {
        const sessionId = rest[0];
        if (!sessionId) {
          throw new Error("events requires a session id");
        }
        const session = engine.getSession(sessionId);
        if (!session) {
          throw new Error(`Session ${sessionId} not found`);
        }
        console.log(JSON.stringify(engine.listEvents(sessionId), null, 2));
        return;
      }
      case "lineage": {
        const sessionId = rest[0];
        if (!sessionId) {
          throw new Error("lineage requires a session id");
        }
        const session = engine.getSession(sessionId);
        if (!session) {
          throw new Error(`Session ${sessionId} not found`);
        }
        console.log(
          JSON.stringify(engine.listSessions({ lineageSessionId: sessionId }), null, 2),
        );
        return;
      }
      case "rerun": {
        const sessionId = rest[0];
        if (!sessionId) {
          throw new Error("rerun requires a session id");
        }

        const session = await engine.rerunSession(sessionId);
        console.log(`Session ${session.id} created from ${sessionId}`);
        const ok = await followSession(engine, session.id);
        process.exitCode = ok ? 0 : 1;
        return;
      }
      case "list": {
        const parsed = parseArgs(rest);
        console.log(JSON.stringify(engine.listSessions(parseSessionListQuery(parsed.flags)), null, 2));
        return;
      }
      case "inbox": {
        const parsed = parseArgs(rest);
        console.log(JSON.stringify(engine.listInbox(parseLimitFlag(parsed.flags.get("limit"))), null, 2));
        return;
      }
      case "config": {
        console.log(JSON.stringify(config, null, 2));
        return;
      }
      default:
        throw new Error(`Unknown command: ${command}`);
    }
  } finally {
    if (command !== "serve") {
      engine.close();
    }
  }
}

main().catch((error) => {
  console.error(error instanceof Error ? error.message : String(error));
  process.exit(1);
});
