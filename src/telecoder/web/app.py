"""TeleCoder Web UI - a simple session viewer and controller."""

from __future__ import annotations

from pathlib import Path

from fastapi import FastAPI, Request
from fastapi.responses import HTMLResponse
from fastapi.templating import Jinja2Templates

from telecoder.config import Config
from telecoder.db import init_db
from telecoder.session import SessionEngine
from telecoder import git_workspace, verify

TEMPLATES_DIR = Path(__file__).parent / "templates"


def create_app(config: Config) -> FastAPI:
    app = FastAPI(title="TeleCoder", version="0.1.0")
    templates = Jinja2Templates(directory=str(TEMPLATES_DIR))

    init_db(config.storage.db_full_path)
    engine = SessionEngine(config)

    @app.get("/", response_class=HTMLResponse)
    async def index(request: Request):
        sessions = engine.list()
        # Refresh status of running sessions
        for i, s in enumerate(sessions):
            if s.status == "running":
                sessions[i] = engine.refresh_status(s.id)
        return templates.TemplateResponse("index.html", {
            "request": request,
            "sessions": sessions,
        })

    @app.get("/session/{session_id}", response_class=HTMLResponse)
    async def session_detail(request: Request, session_id: str):
        s = engine.refresh_status(session_id)
        workspace = Path(s.workspace_dir)

        changed_files = []
        diff_summary = ""
        if (workspace / ".git").exists():
            changed_files = git_workspace.get_changed_files(workspace)
            diff_summary = git_workspace.get_diff_summary(workspace)

        stdout_logs = engine.get_logs(session_id, stream="stdout", tail=200)
        stderr_logs = engine.get_logs(session_id, stream="stderr", tail=50)

        return templates.TemplateResponse("session.html", {
            "request": request,
            "session": s,
            "changed_files": changed_files,
            "diff_summary": diff_summary,
            "stdout_logs": stdout_logs,
            "stderr_logs": stderr_logs,
        })

    @app.post("/api/session/{session_id}/stop")
    async def api_stop(session_id: str):
        s = engine.stop(session_id)
        return {"id": s.id, "status": s.status}

    @app.post("/api/session/{session_id}/run")
    async def api_run(request: Request, session_id: str):
        body = await request.json()
        prompt = body.get("prompt", "")
        if not prompt:
            return {"error": "prompt is required"}, 400
        s = engine.run(session_id, prompt)
        return {"id": s.id, "status": s.status, "pid": s.pid}

    @app.get("/api/session/{session_id}/logs")
    async def api_logs(session_id: str, stream: str = "stdout", tail: int = 200):
        logs = engine.get_logs(session_id, stream=stream, tail=tail)
        return {"logs": logs}

    return app
