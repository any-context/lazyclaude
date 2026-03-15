# Issue: ps ループが Node.js event loop をブロック

## 重大度: HIGH

## 問題

`resolveWindow` / `findTmuxWindowForPid` が `execSync` を最大 15 回ループで呼び出す。これは `/notify` HTTP リクエストのハンドラ内で同期的に実行されるため、Node.js event loop が全てブロックされる。

高頻度の通知やシステム負荷時にサーバーが一時的に無応答になる。

## 該当箇所

`scripts/mcp-server.js` — `findTmuxWindowForPid()` および呼び出し元

## 修正方針

- `execSync` を非同期の `exec` に変更し、callback または Promise で処理
- または: PID → window の解決結果をキャッシュして再利用

```js
// 例: Promise ベースに変更
async function findTmuxWindowForPid(pid) {
  // exec を使い event loop をブロックしない
}
```
