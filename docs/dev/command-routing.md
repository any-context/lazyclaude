# コマンドルーティング定義 (現状)

## n (CreateSession)

```
n
├─ [入力]
│  ├─ path = currentProjectRoot()
│  │  ├─ Focus on ProjectNode → project.Path
│  │  ├─ Focus on SessionNode → 親プロジェクトの Path
│  │  └─ Focus なし → filepath.Abs(".")
│  └─ host = resolveHost()
│     ├─ cachedHost != "" → cachedHost
│     └─ pendingHost
└─ [実行] commands.Create({host, path})
   ├─ host == ""
   │  └─ cp.Create(path, "")
   └─ host != ""
      └─ completeRemoteCreate (goroutine)
         ├─ ensureConnected(host)
         ├─ resolveRemotePath(path, host) → queryRemoteCWD
         ├─ remoteAPI.Create(remotePath)
         └─ mirrorMgr.CreateMirror
```

## N (CreateSessionAtCWD)

```
N
├─ [入力]
│  ├─ path = "."
│  └─ host = resolveHost()
└─ [実行] commands.Create({host, "."})
   ├─ host == ""
   │  └─ cp.Create(".", "")
   └─ host != ""
      └─ completeRemoteCreate
         └─ resolveRemotePath(".", host) → queryRemoteCWD
```

## w (CreateWorktree)

```
w → ダイアログ → Enter
├─ [入力]
│  ├─ path = currentProjectRoot()
│  ├─ host = resolveHost()
│  ├─ name = ダイアログ入力
│  └─ prompt = ダイアログ入力
└─ [実行] commands.CreateWorktree({host, path}, name, prompt)
   ├─ prepareRemote(&target)
   │  ├─ ensureConnected(host)
   │  └─ resolveRemotePath(path, host) → queryRemoteCWD
   └─ cp.CreateWorktree(name, prompt, path, host)
```

## W (SelectWorktree)

```
W
├─ [入力]
│  ├─ path = currentProjectRoot()
│  └─ host = resolveHost()
└─ [実行] commands.ListWorktrees({host, path})
   ├─ prepareRemote(&target)
   └─ cp.ListWorktrees(path, host)
```

## P (CreatePMSession)

```
P
├─ [入力]
│  ├─ path = currentProjectRoot()
│  └─ host = resolveHost()
└─ [実行] commands.CreatePMSession({host, path})
   ├─ prepareRemote(&target)
   └─ cp.CreatePMSession(path, host)
```

## d (Delete)

```
d
├─ [入力]
│  └─ id = currentSession().ID
└─ [実行] commands.Delete(id)
   ├─ sess.Host == ""
   │  └─ cp.Delete(id)
   └─ sess.Host != ""
      ├─ rp.Delete(id)
      └─ mirrorMgr.DeleteMirror(id)
```

## R (Rename)

```
R → ダイアログ → Enter
├─ [入力]
│  ├─ id = currentSession().ID
│  └─ newName = ダイアログ入力
└─ [実行] commands.Rename(id, newName)
   ├─ sess.Host == ""
   │  └─ cp.Rename(id, newName)
   └─ sess.Host != ""
      ├─ rp.Rename(id, newName)
      └─ store.UpdateSession + Save
```

## g (LaunchLazygit)

```
g
├─ [入力]
│  ├─ path = currentProjectRoot()
│  └─ host = resolveHost()
└─ [実行] commands.LaunchLazygit({host, path})
   ├─ prepareRemote(&target)
   └─ cp.LaunchLazygit(path, host)
```

## a (Attach)

```
a
├─ [入力]
│  └─ id = currentSession().ID
└─ [実行] cp.AttachSession(id)
```

## Enter (Fullscreen)

```
Enter
├─ [入力]
│  └─ id = currentSession().ID
└─ [実行]
   ├─ capture-pane(mirror window)
   └─ send-keys(mirror window)
```
