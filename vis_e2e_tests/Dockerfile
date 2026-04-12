##############################################
# Dockerfile for lazyclaude visual E2E tests
#
# Targets:
#   remote  — sshd server for SSH tests
#   vhs     — VHS tape recording
#
# Usage (from project root):
#   docker compose -f vis_e2e_tests/docker-compose.ssh.yml build
#   make test-vhs TAPE=ssh_launch
##############################################

# --- Stage: shared base ---
FROM golang:latest AS base
ARG LAZYGIT_VERSION=0.51.1
RUN apt-get update && apt-get install -y --no-install-recommends \
    tmux openssh-server openssh-client curl nodejs expect git \
    && rm -rf /var/lib/apt/lists/*
RUN curl --retry 3 -Lo lazygit.tar.gz "https://github.com/jesseduffield/lazygit/releases/download/v${LAZYGIT_VERSION}/lazygit_${LAZYGIT_VERSION}_Linux_x86_64.tar.gz" && \
    tar xf lazygit.tar.gz lazygit && install lazygit /usr/local/bin && rm -f lazygit lazygit.tar.gz
RUN curl -fsSL https://claude.ai/install.sh | bash
ENV TERM=xterm-256color PATH="/root/.local/bin:${PATH}"

# --- Stage: build lazyclaude binary (shared by remote and vhs) ---
FROM base AS builder
WORKDIR /build
COPY go.mod go.sum ./
COPY third_party/gocui/go.mod third_party/gocui/go.sum third_party/gocui/
COPY third_party/tcell/go.mod third_party/tcell/go.sum third_party/tcell/
RUN go mod download
COPY . .
RUN go build -o bin/lazyclaude ./cmd/lazyclaude

# --- Target: remote (sshd for SSH E2E) ---
FROM base AS remote
COPY --from=builder /build/bin/lazyclaude /usr/local/bin/lazyclaude
COPY --from=builder /build/lazyclaude.tmux /opt/lazyclaude/lazyclaude.tmux
COPY --from=builder /build/scripts/ /opt/lazyclaude/scripts/
RUN ssh-keygen -A && mkdir -p /run/sshd
RUN sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PermitEmptyPasswords.*/PermitEmptyPasswords yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    echo 'GatewayPorts yes' >> /etc/ssh/sshd_config && \
    passwd -d root
RUN echo 'export PATH="/root/.local/bin:$PATH"' >> /root/.bashrc && \
    git config --global user.email "test@test.com" && \
    git config --global user.name "test"
RUN echo '{"hasCompletedOnboarding":true,"numStartups":10,"projects":{"/":{"hasTrustDialogAccepted":true,"allowedTools":[]}}}' > /root/.claude.json
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D"]

# --- Target: vhs (VHS has its own base due to chromium/ttyd/ffmpeg deps) ---
FROM ghcr.io/charmbracelet/vhs:latest AS vhs
ARG LAZYGIT_VERSION=0.51.1
RUN apt-get update && apt-get install -y --no-install-recommends \
    tmux golang-go git nodejs curl openssh-client locales \
    && rm -rf /var/lib/apt/lists/* \
    && sed -i '/en_US.UTF-8/s/^# //' /etc/locale.gen && locale-gen
RUN curl --retry 3 -Lo lazygit.tar.gz "https://github.com/jesseduffield/lazygit/releases/download/v${LAZYGIT_VERSION}/lazygit_${LAZYGIT_VERSION}_Linux_x86_64.tar.gz" && \
    tar xf lazygit.tar.gz lazygit && install lazygit /usr/local/bin && rm -f lazygit lazygit.tar.gz
ENV TERM=xterm-256color LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8
RUN mkdir -p /root/.ssh && printf 'Host *\n  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null\n' > /root/.ssh/config
WORKDIR /app
COPY go.mod go.sum ./
COPY third_party/gocui/go.mod third_party/gocui/go.sum third_party/gocui/
COPY third_party/tcell/go.mod third_party/tcell/go.sum third_party/tcell/
RUN go mod download
COPY . .
COPY --from=builder /build/bin/lazyclaude /app/bin/lazyclaude
RUN curl -fsSL https://claude.ai/install.sh | bash
ENV PATH="/root/.local/bin:${PATH}"
RUN echo '{"hasCompletedOnboarding":true,"numStartups":10,"projects":{"/":{"hasTrustDialogAccepted":true,"allowedTools":[]}}}' > /root/.claude.json
RUN ln -sf /app/bin/lazyclaude /usr/local/bin/lazyclaude && \
    mkdir -p /app/outputs /tmp/lazyclaude && \
    echo 'export PATH="/root/.local/bin:$PATH"' >> /root/.zshrc && \
    echo 'export PATH="/root/.local/bin:$PATH"' >> /root/.bashrc && \
    echo 'alias lazyclaude="/app/bin/lazyclaude --debug"' >> /root/.bashrc && \
    mv /bin/bash /bin/bash-real && \
    printf '#!/bin/bash-real\nif [ -z "$VHS_IN_TMUX" ]; then\n  export VHS_IN_TMUX=1\n  /app/bin/lazyclaude setup 2>/dev/null\n  tmux new-session -d -s main\n  bash-real /app/lazyclaude.tmux 2>/dev/null\n  exec tmux attach -t main\nfi\nexec /bin/bash-real "$@"\n' > /bin/bash && \
    chmod +x /bin/bash
ENTRYPOINT ["/app/vis_e2e_tests/entrypoint.sh"]
