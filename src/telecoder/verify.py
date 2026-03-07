"""Verification layer - run tests and linters, capture results."""

from __future__ import annotations

import subprocess
from dataclasses import dataclass
from pathlib import Path


@dataclass
class VerifyResult:
    command: str
    passed: bool
    return_code: int
    stdout: str
    stderr: str

    @property
    def summary(self) -> str:
        status = "PASS" if self.passed else "FAIL"
        return f"[{status}] {self.command} (exit code {self.return_code})"


def run_command(command: str, workspace_dir: Path, timeout: int = 300) -> VerifyResult:
    """Run a verification command in the workspace."""
    try:
        result = subprocess.run(
            command,
            shell=True,
            cwd=str(workspace_dir),
            capture_output=True,
            text=True,
            timeout=timeout,
        )
        return VerifyResult(
            command=command,
            passed=result.returncode == 0,
            return_code=result.returncode,
            stdout=result.stdout,
            stderr=result.stderr,
        )
    except subprocess.TimeoutExpired:
        return VerifyResult(
            command=command,
            passed=False,
            return_code=-1,
            stdout="",
            stderr=f"Command timed out after {timeout}s",
        )


def run_tests(test_command: str, workspace_dir: Path) -> VerifyResult:
    """Run the test command."""
    return run_command(test_command, workspace_dir)


def run_lint(lint_command: str, workspace_dir: Path) -> VerifyResult:
    """Run the lint command."""
    return run_command(lint_command, workspace_dir)


def verify_session(
    workspace_dir: Path,
    test_command: str | None = None,
    lint_command: str | None = None,
) -> list[VerifyResult]:
    """Run all configured verification steps."""
    results = []
    if test_command:
        results.append(run_tests(test_command, workspace_dir))
    if lint_command:
        results.append(run_lint(lint_command, workspace_dir))
    return results


def format_results(results: list[VerifyResult]) -> str:
    """Format verification results for display."""
    if not results:
        return "No verification commands configured."

    lines = ["Verification Results:", ""]
    for r in results:
        lines.append(r.summary)
        if not r.passed and r.stderr:
            lines.append("")
            lines.append("  stderr:")
            for line in r.stderr.strip().splitlines()[:20]:
                lines.append(f"    {line}")
        if not r.passed and r.stdout:
            lines.append("")
            lines.append("  stdout (last 20 lines):")
            for line in r.stdout.strip().splitlines()[-20:]:
                lines.append(f"    {line}")
        lines.append("")

    all_passed = all(r.passed for r in results)
    lines.append(f"Overall: {'ALL PASSED' if all_passed else 'SOME FAILED'}")
    return "\n".join(lines)
