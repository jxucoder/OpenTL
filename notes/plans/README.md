# TeleCoder Sprint Plan

Date: 2026-03-16

This plan replaces the old Go roadmap. TeleCoder is being rebuilt around:

- TypeScript as the product language
- Bun as the runtime, server, and local SQLite layer
- Node 22 as the adapter/tooling baseline on VPS hosts
- `acpx` as the ACP/session client instead of a custom ACP implementation

## Current Baseline

As of this reset:

- the Go implementation has been removed
- the repo now carries a fresh Bun/TS skeleton
- local state is modeled with Bun SQLite
- the operational host baseline is Bun plus Node 22
- runtime execution is delegated to `acpx exec`
- the existing vision in `notes/vision.md` still stands
- the old Go evaluation report is obsolete except for product lessons

Sprint 0 status:

- the Bun service and CLI now work on the Ubuntu evaluation VPS
- Codex is the current default runtime path through `acpx`
- Opencode and Claude are both validated through explicit agent command
  overrides
- Sprint 2 VPS install and service management are now implemented and verified
- Sprint 3 trust defaults and per-session policy audit metadata are now
  implemented
- Sprint 4 CI watches and watch-triggered run history are now implemented
- Sprint 5 PR review watches and compact return summaries are now implemented
- an inbox-style outcome surface now exists through structured session
  outcomes and `GET /api/inbox`

What still does not exist:

- resume-aware multi-turn session ownership on top of `acpx`
- interactive approval workflows beyond local trust presets
- authenticated webhook verification and scheduler-based watch sources
- user-mediated approval flows around push and PR creation actions

## Sprint Sequence

1. [Sprint 0: Evaluation](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/00-evaluation.md)
   Status note: [Sprint 0 Evaluation Report](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/00-evaluation-report.md)
2. [Sprint 1: Session Core](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/01-session-core.md)
   Status note: [Sprint 1 Session Core Report](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/01-session-core-report.md)
   Milestone note: [Sprint 1 Session Inspection Report](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/01-session-inspection-report.md)
3. [Sprint 2: VPS Productization](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/02-vps-productization.md)
   Status note: [Sprint 2 VPS Productization Report](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/02-vps-productization-report.md)
4. [Sprint 3: Trust and Policy](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/03-trust-and-policy.md)
   Status note: [Sprint 3 Trust and Policy Report](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/03-trust-and-policy-report.md)
5. [Sprint 4: Watch Engine and CI Watch](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/04-watch-engine-and-ci.md)
   Status note: [Sprint 4 Watch Engine and CI Watch Report](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/04-watch-engine-and-ci-report.md)
6. [Sprint 5: PR Watch and Return Experience](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/05-pr-watch-and-return-experience.md)
   Status note: [Sprint 5 PR Watch and Return Experience Report](/Users/jiaruixu/work_space/formal/telecoder/notes/plans/05-pr-watch-and-return-experience-report.md)

## Why This Order

- use `acpx` to collapse ACP complexity early
- get a durable Bun/SQLite local core before building proactive features
- validate one VPS flow end to end before broadening trust surfaces
- choose one runtime path that actually works before optimizing for optionality
- add explicit per-agent launcher overrides when host runtime differences matter
- keep watch and PR automation downstream of a stable session/runtime layer

## Stack Rules

- no custom ACP transport layer unless `acpx` proves insufficient
- prefer Bun built-ins before adding runtime dependencies
- standardize VPS hosts on Node 22 for agent adapter compatibility
- keep the server and CLI in the same TypeScript codebase
- isolate repo writes in TeleCoder-owned workspaces and branches by default
