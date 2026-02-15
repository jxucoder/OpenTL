# OpenTL Sandbox Base Image
#
# This image provides a full development environment with multiple
# coding agents (Codex CLI, OpenCode) inside an isolated Docker container.
#
# Build:  docker build -f docker/base.Dockerfile -t opentl-sandbox .
# Run:    (managed by OpenTL server, not intended for direct use)

FROM ubuntu:24.04

# Expose build-time architecture (amd64 or arm64).
ARG TARGETARCH

# Avoid interactive prompts during package installation.
ENV DEBIAN_FRONTEND=noninteractive

# Install system dependencies.
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    git \
    ca-certificates \
    build-essential \
    jq \
    ripgrep \
    && rm -rf /var/lib/apt/lists/*

# Install Node.js 22 (LTS).
RUN curl -fsSL https://deb.nodesource.com/setup_22.x | bash - \
    && apt-get install -y nodejs \
    && npm install -g pnpm \
    && rm -rf /var/lib/apt/lists/*

# Install Python 3.12.
RUN apt-get update && apt-get install -y --no-install-recommends \
    python3 \
    python3-pip \
    python3-venv \
    && rm -rf /var/lib/apt/lists/*

# Install Go (for Go-based projects and OpenCode).
# Use TARGETARCH so this works on both amd64 and arm64 hosts.
RUN curl -fsSL "https://go.dev/dl/go1.23.4.linux-${TARGETARCH}.tar.gz" | tar -C /usr/local -xzf -
ENV PATH="/usr/local/go/bin:${PATH}"

# --- Coding Agents ---

# 1. Codex CLI (OpenAI) — preferred when OPENAI_API_KEY is set.
RUN npm install -g @openai/codex

# 2. OpenCode (npm) — preferred when ANTHROPIC_API_KEY is set.
#    The actively maintained version is the npm package, not the old Go module.
RUN npm install -g opencode-ai@latest

# Create workspace directory.
WORKDIR /workspace

# Copy entrypoint scripts.
COPY docker/entrypoint.sh /entrypoint.sh
COPY docker/setup.sh /setup.sh
RUN chmod +x /entrypoint.sh /setup.sh

# Environment variables (set by OpenTL server at runtime):
#   OPENTL_SESSION_ID  - Session identifier
#   OPENTL_REPO        - Repository (owner/repo)
#   OPENTL_PROMPT      - Task prompt
#   OPENTL_BRANCH      - Git branch name
#   GITHUB_TOKEN       - GitHub access token
#   ANTHROPIC_API_KEY  - Anthropic API key (optional)
#   OPENAI_API_KEY     - OpenAI API key (optional)

ENTRYPOINT ["/entrypoint.sh"]
