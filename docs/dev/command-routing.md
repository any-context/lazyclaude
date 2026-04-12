# コマンドルーティング (現状)

## n (CreateSession)

```
ステップ1: 入力値の決定

path = currentProjectRoot()
├─ Focus on ProjectNode → project.Path
├─ Focus on SessionNode → 親プロジェクトの Path
└─ Focus なし → filepath.Abs(".")

host = resolveHost()
├─ カーソルがノード上にある
│  ├─ ノードの Host != "" → Host (リモート)
│  └─ ノードの Host == "" → "" (ローカル確定)
└─ カーソルがノード上にない
   └─ pendingHost


ステップ2: ルーティング

commands.Create({host, path})
├─ host == ""
│  └─ cp.Create(path, "")
└─ host != ""
   └─ completeRemoteCreate (goroutine)


ステップ3: リモート実行 (host != "" の場合)

completeRemoteCreate
├─ ensureConnected(host)
├─ resolveRemotePath(path, host)
│  ├─ path == localProjectRoot or path == "." → queryRemoteCWD
│  └─ それ以外 (既存リモートプロジェクトのパス) → path そのまま
├─ remoteAPI.Create(remotePath)
└─ mirrorMgr.CreateMirror
```

## N (CreateSessionAtCWD)

N はペインベース: lazyclaude ペインがある場所 (= pendingHost) で
セッションを作る。カーソルがどのツリーノード上にあるかは参照しない。

```
ステップ1: 入力値の決定

path = "."

host = pendingHost   (ペインベース。resolveHost() は使わない)


ステップ2: ルーティング

commands.Create({host, "."})
├─ host == ""
│  └─ cp.Create(".", "")
└─ host != ""
   └─ completeRemoteCreate (goroutine)


ステップ3: リモート実行 (host != "" の場合)

completeRemoteCreate
├─ ensureConnected(host)
├─ resolveRemotePath(".", host) → queryRemoteCWD
├─ remoteAPI.Create(remotePath)
└─ mirrorMgr.CreateMirror
```

## w (CreateWorktree)

```
ステップ1: 入力値の決定

path = currentProjectRoot()
├─ Focus on ProjectNode → project.Path
├─ Focus on SessionNode → 親プロジェクトの Path
└─ Focus なし → filepath.Abs(".")

host = resolveHost()
├─ カーソルがノード上にある
│  ├─ ノードの Host != "" → Host (リモート)
│  └─ ノードの Host == "" → "" (ローカル確定)
└─ カーソルがノード上にない
   └─ pendingHost

name = ダイアログ入力
prompt = ダイアログ入力


ステップ2: ルーティング

commands.CreateWorktree({host, path}, name, prompt)
├─ prepareRemote(&target)
│  ├─ ensureConnected(host)
│  └─ resolveRemotePath(path, host)
│     ├─ path == localProjectRoot or path == "." → queryRemoteCWD
│     └─ それ以外 → path そのまま
└─ cp.CreateWorktree(name, prompt, path, host)
```

## W (SelectWorktree)

```
ステップ1: 入力値の決定

path = currentProjectRoot()
host = resolveHost()


ステップ2: ルーティング

commands.ListWorktrees({host, path})
├─ prepareRemote(&target)
│  ├─ ensureConnected(host)
│  └─ resolveRemotePath(path, host)
│     ├─ path == localProjectRoot or path == "." → queryRemoteCWD
│     └─ それ以外 → path そのまま
└─ cp.ListWorktrees(path, host)
```

## P (CreatePMSession)

```
ステップ1: 入力値の決定

path = currentProjectRoot()
host = resolveHost()


ステップ2: ルーティング

commands.CreatePMSession({host, path})
├─ prepareRemote(&target)
│  ├─ ensureConnected(host)
│  └─ resolveRemotePath(path, host)
│     ├─ path == localProjectRoot or path == "." → queryRemoteCWD
│     └─ それ以外 → path そのまま
└─ cp.CreatePMSession(path, host)
```

## d (Delete)

```
ステップ1: 入力値の決定

id = currentSession().ID


ステップ2: ルーティング

commands.Delete(id)
├─ sess.Host == ""
│  └─ cp.Delete(id)
└─ sess.Host != ""
   ├─ rp.Delete(id)
   └─ mirrorMgr.DeleteMirror(id)
```

## R (Rename)

```
ステップ1: 入力値の決定

id = currentSession().ID
newName = ダイアログ入力


ステップ2: ルーティング

commands.Rename(id, newName)
├─ sess.Host == ""
│  └─ cp.Rename(id, newName)
└─ sess.Host != ""
   ├─ rp.Rename(id, newName)
   └─ store.UpdateSession + Save
```

## g (LaunchLazygit)

```
ステップ1: 入力値の決定

path = currentProjectRoot()
host = resolveHost()


ステップ2: ルーティング

commands.LaunchLazygit({host, path})
├─ prepareRemote(&target)
│  ├─ ensureConnected(host)
│  └─ resolveRemotePath(path, host)
│     ├─ path == localProjectRoot or path == "." → queryRemoteCWD
│     └─ それ以外 → path そのまま
└─ cp.LaunchLazygit(path, host)
```

## a (Attach)

```
ステップ1: 入力値の決定

id = currentSession().ID


ステップ2: 実行

cp.AttachSession(id)
```

## Enter (Fullscreen)

```
ステップ1: 入力値の決定

id = currentSession().ID


ステップ2: 実行

capture-pane(mirror window)
send-keys(mirror window)
```
