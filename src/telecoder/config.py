"""Configuration loading and defaults."""

from __future__ import annotations

import tomllib
from dataclasses import dataclass, field
from pathlib import Path

DEFAULT_CONFIG_PATHS = [
    Path("/etc/telecoder/config.toml"),
    Path.home() / ".config" / "telecoder" / "config.toml",
    Path("config.toml"),
]

DEFAULT_DATA_DIR = Path("/var/lib/telecoder")


@dataclass
class ServiceConfig:
    host: str = "127.0.0.1"
    port: int = 7830


@dataclass
class RuntimeConfig:
    type: str = "claude-code"
    binary: str | None = None
    env: dict[str, str] = field(default_factory=dict)


@dataclass
class StorageConfig:
    data_dir: Path = field(default_factory=lambda: DEFAULT_DATA_DIR)
    db_path: str = "telecoder.db"

    @property
    def db_full_path(self) -> Path:
        p = Path(self.db_path)
        if p.is_absolute():
            return p
        return self.data_dir / p

    @property
    def workspaces_dir(self) -> Path:
        return self.data_dir / "workspaces"

    @property
    def logs_dir(self) -> Path:
        return self.data_dir / "logs"


@dataclass
class GitConfig:
    branch_prefix: str = "telecoder/"
    auto_push: bool = False


@dataclass
class VerifyConfig:
    test_command: str | None = None
    lint_command: str | None = None


@dataclass
class Config:
    service: ServiceConfig = field(default_factory=ServiceConfig)
    runtime: RuntimeConfig = field(default_factory=RuntimeConfig)
    storage: StorageConfig = field(default_factory=StorageConfig)
    git: GitConfig = field(default_factory=GitConfig)
    verify: VerifyConfig = field(default_factory=VerifyConfig)


def load_config(path: Path | None = None) -> Config:
    """Load config from a TOML file. Falls back to defaults."""
    if path and path.exists():
        return _parse_config(path)

    for candidate in DEFAULT_CONFIG_PATHS:
        if candidate.exists():
            return _parse_config(candidate)

    return Config()


def _parse_config(path: Path) -> Config:
    with open(path, "rb") as f:
        raw = tomllib.load(f)

    svc = raw.get("service", {})
    rt = raw.get("runtime", {})
    st = raw.get("storage", {})
    gt = raw.get("git", {})
    vr = raw.get("verify", {})

    storage = StorageConfig(
        data_dir=Path(st["data_dir"]) if "data_dir" in st else DEFAULT_DATA_DIR,
        db_path=st.get("db_path", "telecoder.db"),
    )

    return Config(
        service=ServiceConfig(
            host=svc.get("host", "127.0.0.1"),
            port=svc.get("port", 7830),
        ),
        runtime=RuntimeConfig(
            type=rt.get("type", "claude-code"),
            binary=rt.get("binary"),
            env=rt.get("env", {}),
        ),
        storage=storage,
        git=GitConfig(
            branch_prefix=gt.get("branch_prefix", "telecoder/"),
            auto_push=gt.get("auto_push", False),
        ),
        verify=VerifyConfig(
            test_command=vr.get("test_command"),
            lint_command=vr.get("lint_command"),
        ),
    )
