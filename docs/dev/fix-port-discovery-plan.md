# Fix: Port/Token Discovery Reliability

**Created**: 2026-03-28
**Status**: Planning
**Branch**: fix-brank-tmux-popup

---

## Problem

Worker/PM が MCP サーバーのポート/トークンを見失う問題が繰り返し発生。

### 原因 1: プロンプトのハードコード値

`worker.md` / `pm.md` の curl コマンドにポートとトークンがハードコードされている:

```bash
curl -H "X-Auth-Token: HARDCODED" http://localhost:HARDCODED_PORT/msg/send
```

サーバー再起動でこれらが stale になる。Connection Recovery セクションはあるが、
LLM が「まずハードコードを試し、失敗を検知し、リカバリーを実行する」必要がある。
今回の問題はまさにこれ — LLM がリカバリーを実行しなかった。

### 原因 2: Hooks の stale lock file 選択

`hooks.go` の JavaScript:

```javascript
const locks = fs.readdirSync(lockDir).filter(f => f.endsWith('.lock'));
const lock = JSON.parse(fs.readFileSync(path.join(lockDir, locks[0])));
const port = parseInt(locks[0], 10);
```

- `locks[0]` はファイルシステム順で最初 — どの lock が現在のサーバーか不明
- stale lock ファイルが残っていると死んだサーバーに接続
- 掃除する仕組みがない

### 原因 3: stale lock ファイルの蓄積

サーバーがクラッシュすると lock ファイルが残る。`RestartServer` は
ポートファイルから読んだポートの lock しか削除しない。他の stale lock は放置。

---

## 修正方針

### Phase 1: プロンプトを動的発見に変更

`worker.md` / `pm.md` の curl を、毎回ポートファイルとロックファイルから
動的に発見するスクリプトに置き換える。ハードコード値を排除。

**Before** (worker.md):
```bash
# Hardcoded — stale after restart
curl -H "X-Auth-Token: %s" http://localhost:%d/msg/send
# ... Connection Recovery as fallback
```

**After** (worker.md):
```bash
# Always discovers current server
PORT=$(cat %s) && \
TOKEN=$(python3 -c "import json,sys; print(json.load(open(sys.argv[1]))['authToken'])" "%s/$PORT.lock") && \
curl -s -X POST -H "X-Auth-Token: $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"from":"%s","to":"<recipient-id>","type":"review_request","body":"<description>"}' \
  "http://localhost:$PORT/msg/send"
```

Connection Recovery セクションは不要になる（curl 自体が動的発見）。
portFile と ideDir はテンプレートパラメータとして渡す。

**対象ファイル**:
- `internal/session/prompts/worker.md`
- `internal/session/prompts/pm.md`
- `internal/session/role.go` — `BuildWorkerPrompt` / `BuildPMPrompt` のパラメータ調整

### Phase 2: Hooks の lock file 選択を堅牢化

hooks.go の JavaScript を修正:
1. lock ファイルから PID を読み、`kill -0 PID` で生存確認
2. 生存している lock のみ使用
3. 複数生存なら最新（port 番号が最大 = 最後に起動）を選択

**対象ファイル**:
- `internal/core/config/hooks.go` — `preToolUseHookCommand`, `notificationHookCommand`

### Phase 3: サーバー起動時に stale lock を掃除

`startServer` / `Server.Start` で、自分以外の lock ファイルを検査し、
PID が死んでいるものを削除。

**対象ファイル**:
- `internal/server/lock.go` — `CleanStale()` メソッド追加
- `internal/server/server.go` — `Start()` 内で呼び出し

---

## 変更ファイル一覧

| File | Phase | Change |
|------|-------|--------|
| `internal/session/prompts/worker.md` | 1 | curl を動的発見に変更 |
| `internal/session/prompts/pm.md` | 1 | 同上 |
| `internal/session/role.go` | 1 | BuildWorkerPrompt/BuildPMPrompt のパラメータ調整 |
| `internal/session/role_test.go` | 1 | テスト更新 |
| `internal/core/config/hooks.go` | 2 | lock 選択ロジック改善 |
| `internal/core/config/hooks_test.go` | 2 | テスト更新 |
| `internal/server/lock.go` | 3 | CleanStale() 追加 |
| `internal/server/server.go` | 3 | Start() で CleanStale() 呼び出し |

## Testing

```bash
go test ./internal/... -count=1 -race
```

## Risks

| Risk | Severity | Mitigation |
|------|----------|------------|
| 動的発見が遅い (python3 起動) | Low | 1 回の curl あたり ~50ms。許容範囲 |
| portFile が存在しない | Low | cat 失敗で curl 実行されず。エラーメッセージが出る |
| hooks の kill -0 が macOS で動作するか | Low | process.kill(pid, 0) は POSIX 標準 |
