import { useEffect, useState } from "react";

// Types matching the TeleCoder API.
interface Session {
  id: string;
  repo: string;
  prompt: string;
  mode: "task" | "chat";
  status: "pending" | "running" | "complete" | "error" | "idle";
  branch: string;
  agent?: string;
  chain_id?: string;
  chain_depth?: number;
  pr_url?: string;
  pr_number?: number;
  result?: { type: string; content?: string; pr_url?: string; pr_number?: number };
  error?: string;
  created_at: string;
  updated_at: string;
}

const API_URL = import.meta.env.VITE_API_URL || "http://localhost:7080";

function App() {
  const [sessions, setSessions] = useState<Session[]>([]);
  const [loading, setLoading] = useState(true);
  const [prompt, setPrompt] = useState("");
  const [repo, setRepo] = useState("");
  const [creating, setCreating] = useState(false);
  const [selectedSession, setSelectedSession] = useState<string | null>(null);
  const [logs, setLogs] = useState<string[]>([]);

  // Fetch sessions on mount and every 5 seconds.
  useEffect(() => {
    const fetchSessions = async () => {
      try {
        const resp = await fetch(`${API_URL}/api/sessions`);
        const data = await resp.json();
        setSessions(data);
      } catch (err) {
        console.error("Failed to fetch sessions:", err);
      } finally {
        setLoading(false);
      }
    };

    fetchSessions();
    const interval = setInterval(fetchSessions, 5000);
    return () => clearInterval(interval);
  }, []);

  // Stream SSE events for selected session.
  useEffect(() => {
    if (!selectedSession) return;

    setLogs([]);
    const eventSource = new EventSource(
      `${API_URL}/api/sessions/${selectedSession}/events`
    );

    eventSource.onmessage = (event) => {
      try {
        const data = JSON.parse(event.data);
        setLogs((prev) => [...prev, `[${data.type}] ${data.data}`]);
      } catch {
        // Ignore parse errors.
      }
    };

    eventSource.onerror = () => {
      eventSource.close();
    };

    return () => eventSource.close();
  }, [selectedSession]);

  const createSession = async () => {
    if (!prompt || !repo) return;
    setCreating(true);
    try {
      const resp = await fetch(`${API_URL}/api/sessions`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ repo, prompt }),
      });
      const data = await resp.json();
      setSelectedSession(data.id);
      setPrompt("");
      // Refresh session list.
      const listResp = await fetch(`${API_URL}/api/sessions`);
      setSessions(await listResp.json());
    } catch (err) {
      console.error("Failed to create session:", err);
    } finally {
      setCreating(false);
    }
  };

  const statusBadge = (status: string) => {
    const colors: Record<string, string> = {
      pending: "bg-yellow-100 text-yellow-800",
      running: "bg-blue-100 text-blue-800",
      complete: "bg-green-100 text-green-800",
      error: "bg-red-100 text-red-800",
      idle: "bg-gray-100 text-gray-800",
    };
    return (
      <span
        className={`px-2 py-1 rounded-full text-xs font-medium ${colors[status] || "bg-gray-100"}`}
      >
        {status}
      </span>
    );
  };

  return (
    <div className="min-h-screen bg-gray-50">
      {/* Header */}
      <header className="bg-white border-b border-gray-200 px-6 py-4">
        <div className="max-w-7xl mx-auto flex items-center justify-between">
          <div>
            <h1 className="text-xl font-bold text-gray-900">TeleCoder</h1>
            <p className="text-sm text-gray-500">Background Coding Agent</p>
          </div>
          <a
            href="https://github.com/jxucoder/TeleCoder"
            className="text-sm text-gray-500 hover:text-gray-900"
            target="_blank"
            rel="noreferrer"
          >
            GitHub
          </a>
        </div>
      </header>

      <main className="max-w-7xl mx-auto px-6 py-8">
        {/* New Session Form */}
        <div className="bg-white rounded-lg border border-gray-200 p-6 mb-8">
          <h2 className="text-lg font-semibold mb-4">New Task</h2>
          <div className="flex gap-4">
            <input
              type="text"
              placeholder="owner/repo"
              value={repo}
              onChange={(e) => setRepo(e.target.value)}
              className="px-3 py-2 border border-gray-300 rounded-md text-sm w-48 focus:outline-none focus:ring-2 focus:ring-blue-500"
            />
            <input
              type="text"
              placeholder="Describe the task..."
              value={prompt}
              onChange={(e) => setPrompt(e.target.value)}
              className="flex-1 px-3 py-2 border border-gray-300 rounded-md text-sm focus:outline-none focus:ring-2 focus:ring-blue-500"
              onKeyDown={(e) => e.key === "Enter" && createSession()}
            />
            <button
              onClick={createSession}
              disabled={creating || !prompt || !repo}
              className="px-4 py-2 bg-blue-600 text-white rounded-md text-sm font-medium hover:bg-blue-700 disabled:opacity-50 disabled:cursor-not-allowed"
            >
              {creating ? "Starting..." : "Run"}
            </button>
          </div>
        </div>

        <div className="grid grid-cols-3 gap-8">
          {/* Session List */}
          <div className="col-span-1">
            <h2 className="text-lg font-semibold mb-4">Sessions</h2>
            {loading ? (
              <p className="text-gray-500 text-sm">Loading...</p>
            ) : sessions.length === 0 ? (
              <p className="text-gray-500 text-sm">No sessions yet.</p>
            ) : (
              <div className="space-y-2">
                {sessions.map((session) => (
                  <button
                    key={session.id}
                    onClick={() => setSelectedSession(session.id)}
                    className={`w-full text-left p-3 rounded-lg border ${
                      selectedSession === session.id
                        ? "border-blue-500 bg-blue-50"
                        : "border-gray-200 bg-white hover:bg-gray-50"
                    }`}
                  >
                    <div className="flex items-center justify-between mb-1">
                      <span className="text-sm font-mono text-gray-600">
                        {session.id}
                      </span>
                      {statusBadge(session.status)}
                    </div>
                    <p className="text-sm text-gray-900 truncate">
                      {session.prompt}
                    </p>
                    <div className="flex items-center gap-2 mt-1">
                      <p className="text-xs text-gray-500">{session.repo}</p>
                      {session.agent && session.agent !== "auto" && (
                        <span className="px-1.5 py-0.5 bg-purple-100 text-purple-700 rounded text-xs">
                          {session.agent}
                        </span>
                      )}
                      {session.chain_id && (
                        <span className="px-1.5 py-0.5 bg-orange-100 text-orange-700 rounded text-xs" title={`Chain: ${session.chain_id} (depth ${session.chain_depth})`}>
                          🔗 {session.chain_depth}
                        </span>
                      )}
                    </div>
                    {session.pr_url && (
                      <a
                        href={session.pr_url}
                        target="_blank"
                        rel="noreferrer"
                        className="text-xs text-blue-600 hover:underline mt-1 block"
                        onClick={(e) => e.stopPropagation()}
                      >
                        PR #{session.pr_number}
                      </a>
                    )}
                  </button>
                ))}
              </div>
            )}
          </div>

          {/* Log Viewer */}
          <div className="col-span-2">
            <h2 className="text-lg font-semibold mb-4">
              {selectedSession ? `Logs: ${selectedSession}` : "Logs"}
            </h2>
            {selectedSession ? (
              <div className="bg-gray-900 rounded-lg p-4 h-[600px] overflow-y-auto font-mono text-sm">
                {logs.length === 0 ? (
                  <p className="text-gray-500">Waiting for events...</p>
                ) : (
                  logs.map((line, i) => (
                    <div
                      key={i}
                      className={`py-0.5 ${
                        line.startsWith("[status]")
                          ? "text-cyan-400"
                          : line.startsWith("[error]")
                            ? "text-red-400"
                            : line.startsWith("[done]")
                              ? "text-green-400"
                              : "text-gray-300"
                      }`}
                    >
                      {line}
                    </div>
                  ))
                )}
              </div>
            ) : (
              <div className="bg-gray-100 rounded-lg p-8 text-center text-gray-500">
                Select a session to view logs
              </div>
            )}
          </div>
        </div>
      </main>
    </div>
  );
}

export default App;
