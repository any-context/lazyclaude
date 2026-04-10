# コマンドルーティング (現状)

## n (CreateSession)

```
n
├─ path = currentProjectRoot()
│  ├─ Focus on ProjectNode → project.Path
│  ├─ Focus on SessionNode → 親プロジェクトの Path
│  └─ Focus なし → filepath.Abs(".")
├─ host = resolveHost()
│  ├─ cachedHost != "" → cachedHost
│  └─ pendingHost
└─ commands.Create({host, path})
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
├─ path = "."
├─ host = resolveHost()
└─ commands.Create({host, "."})
   ├─ host == ""
   │  └─ cp.Create(".", "")
   └─ host != ""
      └─ completeRemoteCreate
         └─ resolveRemotePath(".", host) → queryRemoteCWD
```

## w (CreateWorktree)

```
w → ダイアログ → Enter
├─ path = currentProjectRoot()
├─ host = resolveHost()
├─ name = ダイアログ入力
├─ prompt = ダイアログ入力
└─ commands.CreateWorktree({host, path}, name, prompt)
   ├─ prepareRemote(&target)
   │  ├─ ensureConnected(host)
   │  └─ resolveRemotePath(path, host) → queryRemoteCWD
   └─ cp.CreateWorktree(name, prompt, path, host)
```

## W (SelectWorktree)

```
W
├─ path = currentProjectRoot()
├─ host = resolveHost()
└─ commands.ListWorktrees({host, path})
   ├─ prepareRemote(&target)
   └─ cp.ListWorktrees(path, host)
```

## P (CreatePMSession)

```
P
├─ path = currentProjectRoot()
├─ host = resolveHost()
└─ commands.CreatePMSession({host, path})
   ├─ prepareRemote(&target)
   └─ cp.CreatePMSession(path, host)
```

## d (Delete)

```
d
├─ id = currentSession().ID
└─ commands.Delete(id)
   ├─ sess.Host == ""
   │  └─ cp.Delete(id)
   └─ sess.Host != ""
      ├─ rp.Delete(id)
      └─ mirrorMgr.DeleteMirror(id)
```

## R (Rename)

```
R → ダイアログ → Enter
├─ id = currentSession().ID
├─ newName = ダイアログ入力
└─ commands.Rename(id, newName)
   ├─ sess.Host == ""
   │  └─ cp.Rename(id, newName)
   └─ sess.Host != ""
      ├─ rp.Rename(id, newName)
      └─ store.UpdateSession + Save
```

## g (LaunchLazygit)

```
g
├─ path = currentProjectRoot()
├─ host = resolveHost()
└─ commands.LaunchLazygit({host, path})
   ├─ prepareRemote(&target)
   └─ cp.LaunchLazygit(path, host)
```

## a (Attach)

```
a
├─ id = currentSession().ID
└─ cp.AttachSession(id)
```

## Enter (Fullscreen)

```
Enter
├─ id = currentSession().ID
├─ capture-pane(mirror window)
└─ send-keys(mirror window)
```
