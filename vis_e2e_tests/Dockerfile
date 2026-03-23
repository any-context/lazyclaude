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
RUN apt-get update && apt-get install -y --no-install-recommends \
    tmux openssh-server openssh-client curl nodejs expect git \
    && rm -rf /var/lib/apt/lists/*
RUN LAZYGIT_VERSION=$(curl -s "https://api.github.com/repos/jesseduffield/lazygit/releases/latest" | grep -Po '"tag_name": "v\K[^"]*') && \
    curl -Lo lazygit.tar.gz "https://github.com/jesseduffield/lazygit/releases/latest/download/lazygit_${LAZYGIT_VERSION}_Linux_x86_64.tar.gz" && \
    tar xf lazygit.tar.gz lazygit && install lazygit /usr/local/bin && rm -f lazygit lazygit.tar.gz
RUN curl -fsSL https://claude.ai/install.sh | bash
ENV TERM=xterm-256color PATH="/root/.local/bin:${PATH}"

# --- Target: remote (sshd for SSH E2E) ---
FROM base AS remote
RUN ssh-keygen -A && mkdir -p /run/sshd
RUN sed -i 's/#PermitRootLogin.*/PermitRootLogin yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PermitEmptyPasswords.*/PermitEmptyPasswords yes/' /etc/ssh/sshd_config && \
    sed -i 's/#PasswordAuthentication.*/PasswordAuthentication yes/' /etc/ssh/sshd_config && \
    echo 'GatewayPorts yes' >> /etc/ssh/sshd_config && \
    passwd -d root
RUN echo 'export PATH="/root/.local/bin:$PATH"' >> /root/.bashrc
COPY vis_e2e_tests/claude-settings.json /root/.claude/settings.json
COPY vis_e2e_tests/hooks/ /root/.claude/hooks/
RUN echo '{"hasCompletedOnboarding":true,"numStartups":10,"projects":{"/":{"hasTrustDialogAccepted":true,"allowedTools":[]}}}' > /root/.claude.json
EXPOSE 22
CMD ["/usr/sbin/sshd", "-D"]

# --- Target: vhs (VHS has its own base due to chromium/ttyd/ffmpeg deps) ---
FROM ghcr.io/charmbracelet/vhs:latest AS vhs
RUN apt-get update && apt-get install -y --no-install-recommends \
    tmux golang-go git nodejs curl openssh-client locales \
    && rm -rf /var/lib/apt/lists/* \
    && sed -i '/en_US.UTF-8/s/^# //' /etc/locale.gen && locale-gen
RUN LAZYGIT_VERSION=$(curl -s "https://api.github.com/repos/jesseduffield/lazygit/releases/latest" | grep -Po '"tag_name": "v\K[^"]*') && \
    curl -Lo lazygit.tar.gz "https://github.com/jesseduffield/lazygit/releases/latest/download/lazygit_${LAZYGIT_VERSION}_Linux_x86_64.tar.gz" && \
    tar xf lazygit.tar.gz lazygit && install lazygit /usr/local/bin && rm -f lazygit lazygit.tar.gz
ENV TERM=xterm-256color LANG=en_US.UTF-8 LC_ALL=en_US.UTF-8
RUN mkdir -p /root/.ssh && printf 'Host *\n  StrictHostKeyChecking no\n  UserKnownHostsFile /dev/null\n' > /root/.ssh/config
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN curl -fsSL https://claude.ai/install.sh | bash
ENV PATH="/root/.local/bin:${PATH}"
COPY vis_e2e_tests/claude-settings.json /root/.claude/settings.json
COPY vis_e2e_tests/hooks/ /root/.claude/hooks/
RUN echo '{"hasCompletedOnboarding":true,"numStartups":10,"projects":{"/":{"hasTrustDialogAccepted":true,"allowedTools":[]}}}' > /root/.claude.json
RUN go build -o bin/lazyclaude ./cmd/lazyclaude && \
    ln -sf /app/bin/lazyclaude /usr/local/bin/lazyclaude && \
    mkdir -p /app/outputs /tmp/lazyclaude && \
    echo 'export PATH="/root/.local/bin:$PATH"' >> /root/.zshrc && \
    echo 'export PATH="/root/.local/bin:$PATH"' >> /root/.bashrc && \
    echo 'alias lazyclaude="/app/bin/lazyclaude --debug"' >> /root/.bashrc && \
    mv /bin/bash /bin/bash-real && \
    printf '#!/bin/bash-real\nif [ -z "$VHS_IN_TMUX" ]; then\n  export VHS_IN_TMUX=1\n  /app/bin/lazyclaude setup 2>/dev/null\n  tmux new-session -d -s main\n  bash-real /app/lazyclaude.tmux 2>/dev/null\n  exec tmux attach -t main\nfi\nexec /bin/bash-real "$@"\n' > /bin/bash && \
    chmod +x /bin/bash
ENTRYPOINT ["/app/vis_e2e_tests/entrypoint.sh"]
