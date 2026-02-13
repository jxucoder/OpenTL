# OpenTL Sandbox Base Image
#
# This image provides a full development environment for running
# the OpenCode coding agent inside an isolated Docker container.
#
# Build:  docker build -f docker/base.Dockerfile -t opentl-sandbox .
# Run:    (managed by OpenTL server, not intended for direct use)

FROM ubuntu:24.04

# Avoid interactive prompts during package installation.
ENV DEBIAN_FRONTEND=noninteractive

# Install system dependencies.
RUN apt-get update && apt-get install -y --no-install-recommends \
    curl \
    git \
    ca-certificates \
    build-essential \
    jq \
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

# Install Go (for OpenCode and Go-based projects).
RUN curl -fsSL https://go.dev/dl/go1.23.4.linux-amd64.tar.gz | tar -C /usr/local -xzf -
ENV PATH="/usr/local/go/bin:${PATH}"

# Install OpenCode (the coding agent that runs inside the sandbox).
RUN go install github.com/nicepkg/opencode@latest
ENV PATH="/root/go/bin:${PATH}"

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
