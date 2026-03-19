---
name: Claude Code install method
description: npm install of Claude Code is prohibited - must use standalone installer
type: feedback
---

Claude Code のインストールに npm (`npm install -g @anthropic-ai/claude-code`) は使用禁止。
スタンドアローンインストーラーを使う。

**Why:** スタンドアローンバイナリ版が存在し、npm 経由は非推奨。
**How to apply:** Dockerfile で Claude Code をインストールする際は `curl -sL https://claude.ai/install.sh | bash` を使用。Cloudflare がブロックする場合は、ビルド済みイメージからコピーするか、ホストでダウンロードして COPY する。