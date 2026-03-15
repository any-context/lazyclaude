#!/usr/bin/env node
'use strict';

/**
 * tmux-claude MCP server
 *
 * Persistent WebSocket MCP server that Claude CLI connects to.
 * Started once from .zshrc via tmux-claude.zsh.
 *
 * Detects openDiff calls (file writes) and shows a tmux popup
 * so the user can review/interact with Claude.
 *
 * Runtime files:
 *   /tmp/tmux-claude-mcp.pid   - server PID
 *   /tmp/tmux-claude-mcp.port  - listening port
 *   /tmp/tmux-claude-mcp.token - auth token
 *   ~/.claude/ide/<port>.lock  - Claude discovery lock file
 */

const net = require('node:net');
const crypto = require('node:crypto');
const fs = require('node:fs');
const path = require('node:path');
const os = require('node:os');
const { execSync, spawnSync, spawn } = require('node:child_process');

// --- constants ---

const LOCK_DIR = path.join(os.homedir(), '.claude', 'ide');
const PID_FILE = '/tmp/tmux-claude-mcp.pid';
const PORT_FILE = '/tmp/tmux-claude-mcp.port';
const TOKEN_FILE = '/tmp/tmux-claude-mcp.token';
const WS_MAGIC = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';

// 再起動をまたいで token を保持（同じ token で Claude Code が自動再接続できる）
const AUTH_TOKEN = process.env.TMUX_CLAUDE_TOKEN || (() => {
  try { return fs.readFileSync(TOKEN_FILE, 'utf8').trim(); } catch {}
  return crypto.randomUUID();
})();

// シェルのシングルクォートエスケープ（-E オプション等でシェル経由で実行される文字列に使用）
function shellQuote(s) {
  return "'" + String(s).replace(/'/g, "'\\''") + "'";
}

// diff popup の選択を permission dialog の send-keys に渡すための一時保管
const pendingDiffChoices = new Map(); // window → choice ('1'|'2'|'3')

// PreToolUse hook から受け取ったツール情報（Notification 時に popup に渡す）
const pendingToolInfo = new Map(); // window → {tool_name, tool_input, ts}

// 期限切れエントリを定期削除（メモリリーク防止）
setInterval(() => {
  const cutoff = Date.now() - 15000;
  for (const [key, val] of pendingToolInfo) {
    if (val.ts < cutoff) pendingToolInfo.delete(key);
  }
}, 60_000).unref();

// --- WebSocket helpers ---

function wsAccept(key) {
  return crypto.createHash('sha1').update(key + WS_MAGIC).digest('base64');
}

function send101(socket, key) {
  socket.write(
    'HTTP/1.1 101 Switching Protocols\r\n' +
    'Upgrade: websocket\r\n' +
    'Connection: Upgrade\r\n' +
    `Sec-WebSocket-Accept: ${wsAccept(key)}\r\n` +
    '\r\n',
  );
}

function sendHttpError(socket, status, msg) {
  socket.end(`HTTP/1.1 ${status} ${msg}\r\n\r\n`);
}

function sendText(socket, text) {
  const payload = Buffer.from(text, 'utf8');
  let header;
  if (payload.length < 126) {
    header = Buffer.from([0x81, payload.length]);
  } else if (payload.length < 65536) {
    header = Buffer.allocUnsafe(4);
    header[0] = 0x81;
    header[1] = 126;
    header.writeUInt16BE(payload.length, 2);
  } else {
    header = Buffer.allocUnsafe(10);
    header[0] = 0x81;
    header[1] = 127;
    header.writeBigUInt64BE(BigInt(payload.length), 2);
  }
  socket.write(Buffer.concat([header, payload]));
}

function sendPong(socket, payload) {
  const header = Buffer.from([0x8a, payload.length]);
  socket.write(Buffer.concat([header, payload]));
}

function parseFrame(buf) {
  if (buf.length < 2) return null;

  const masked = (buf[1] & 0x80) !== 0;
  let len = buf[1] & 0x7f;
  let offset = 2;

  if (len === 126) {
    if (buf.length < 4) return null;
    len = buf.readUInt16BE(2);
    offset = 4;
  } else if (len === 127) {
    if (buf.length < 10) return null;
    len = Number(buf.readBigUInt64BE(2));
    offset = 10;
  }

  const total = offset + (masked ? 4 : 0) + len;
  if (buf.length < total) return null;

  let payload;
  if (masked) {
    const mask = buf.slice(offset, offset + 4);
    offset += 4;
    payload = Buffer.allocUnsafe(len);
    for (let i = 0; i < len; i++) payload[i] = buf[offset + i] ^ mask[i % 4];
  } else {
    payload = buf.slice(offset, offset + len);
  }

  return { opcode: buf[0] & 0x0f, payload, consumed: total };
}

// --- JSON-RPC helper ---

function reply(socket, id, result) {
  sendText(socket, JSON.stringify({ jsonrpc: '2.0', id, result }));
}

// --- Per-connection state ---

const socketState = new Map(); // socket → { pid: number | null }
const pidToWindow = new Map(); // pid → window name

// --- Popup ---

function findActiveClient() {
  try {
    // タブ区切りでパース（セッション名にスペースが含まれる場合を考慮）
    const out = execSync('tmux list-clients -F "#{client_name}\t#{client_session}\t#{client_activity}"', { encoding: 'utf8' });
    const clients = out.trim().split('\n')
      .filter(Boolean)
      .map(l => { const [name, sess, act] = l.split('\t'); return { name, sess, activity: Number(act) }; })
      .sort((a, b) => b.activity - a.activity);
    // claude セッションのクライアントを優先（Claude Code が動いているセッション）
    return (clients.find(c => c.sess === 'claude') ?? clients[0])?.name ?? null;
  } catch { return null; }
}

function findTmuxWindowForPid(pid) {
  const paneMap = new Map();
  try {
    for (const line of execSync('tmux list-panes -a -F "#{pane_pid}\t#{session_name}\t#{window_name}"', { encoding: 'utf8' }).trim().split('\n').filter(Boolean)) {
      const [panePid, session, window] = line.split('\t');
      paneMap.set(panePid, { session, window });
    }
  } catch { return null; }

  let current = String(pid);
  for (let i = 0; i < 15; i++) {
    if (paneMap.has(current)) return paneMap.get(current);
    try {
      const ppid = execSync(`ps -o ppid= -p ${current} 2>/dev/null`, { encoding: 'utf8' }).trim();
      if (!ppid || ppid === '1' || ppid === '0' || ppid === current) break;
      current = ppid;
    } catch { break; }
  }
  return null;
}

// PID から tmux window 名を解決（WebSocket 接続なしでも動作する）
function resolveWindow(rawPid) {
  // 1. pidToWindow（WebSocket ide_connected で登録済み）を優先
  let current = rawPid;
  for (let i = 0; i < 15; i++) {
    if (pidToWindow.has(current)) return pidToWindow.get(current);
    try {
      const ppid = execSync(`ps -o ppid= -p ${current} 2>/dev/null`, { encoding: 'utf8' }).trim();
      if (!ppid || ppid === '1' || ppid === '0' || ppid === String(current)) break;
      current = Number(ppid);
    } catch { break; }
  }
  // 2. フォールバック: tmux ペインを直接スキャン
  const info = findTmuxWindowForPid(rawPid);
  return info?.session === 'claude' ? info.window : null;
}

// Permission dialog の選択肢数を capture-pane の出力から検出
// Claude Code のダイアログは "1." "2." ... のように番号付きで表示される
function detectMaxOption(paneContent) {
  let max = 0;
  for (const line of paneContent.split('\n')) {
    const m = line.match(/^\s*(?:[❯>]\s+)?(\d+)[.)]/);
    if (m) max = Math.max(max, Number(m[1]));
  }
  return max > 0 ? max : 3; // 検出失敗時は 3 をデフォルト（Edit/Write 等）
}

// ツール名・入力から popup サイズ (%単位) を推定
function estimateToolPopupSize(toolName, toolInput, termW, termH, hasCwd = false, cwdLen = 0) {
  let lines = 1; // ヘッダー行（tool name）
  // アクションバー最低幅: "  Yes: y  |  Allow always: a  |  No: n  |  cancel: Esc" ≈ 54 chars
  let maxLen = 54;

  const clampLines = (arr, limit) => Math.min(arr.length, limit);
  const maxLineLen = (arr) => arr.reduce((m, l) => Math.max(m, l.length), 0);

  switch (toolName) {
    case 'Bash': {
      const cmd = (toolInput.command ?? '').split('\n');
      lines += clampLines(cmd, 20);
      maxLen = Math.max(maxLen, maxLineLen(cmd.slice(0, 20)));
      break;
    }
    case 'Read':
      lines += 1;
      maxLen = Math.max(maxLen, (toolInput.file_path ?? '').length);
      break;
    case 'Write':
      lines += 2;
      maxLen = Math.max(maxLen, (toolInput.file_path ?? '').length);
      break;
    case 'Edit': {
      const fp = toolInput.file_path ?? '';
      const old = (toolInput.old_string ?? '').split('\n').slice(0, 5);
      const nw  = (toolInput.new_string ?? '').split('\n').slice(0, 5);
      lines += 1 + 1 + old.length + 1 + nw.length;
      maxLen = Math.max(maxLen, fp.length, maxLineLen(old), maxLineLen(nw));
      break;
    }
    case 'Agent':
    case 'Task': {
      const prompt = (toolInput.prompt ?? toolInput.description ?? '').split('\n').slice(0, 10);
      lines += prompt.length;
      maxLen = Math.max(maxLen, maxLineLen(prompt));
      break;
    }
    default: {
      const entries = Object.entries(toolInput ?? {}).slice(0, 8);
      lines += entries.length;
      for (const [k, v] of entries) {
        const val = typeof v === 'string' ? v : JSON.stringify(v);
        maxLen = Math.max(maxLen, k.length + 2 + val.split('\n')[0].length);
      }
    }
  }

  if (hasCwd) {
    lines += 1; // CWD 行（claude-tool-popup.js が先頭に追加）
    maxLen = Math.max(maxLen, cwdLen); // CWD 行の長さも幅に反映
  }
  lines += 3; // セパレーター + アクションバー + プロンプト行

  const wPct = termW > 0 ? Math.min(95, Math.max(25, Math.round((maxLen + 8) / termW * 100))) : 70;
  // +3: tmux ボーダー上下(2) + ❯partial行(1) 分、ceil で切り上げて不足を防ぐ
  const hPct = termH > 0 ? Math.min(90, Math.max(10, Math.ceil((lines + 3)    / termH * 100))) : 60;
  return { wPct, hPct };
}

function getNotifyType() {
  try {
    const val = execSync('tmux show-option -gv @claude-notify-type 2>/dev/null', { encoding: 'utf8' }).trim();
    return val === 'menu' ? 'menu' : 'popup';
  } catch { return 'popup'; }
}

// Builds new file contents from tool_input for diff popup.
// Returns { newContents, oldContent } — oldContent is null for Write or on read failure.
// newContents is null when not computable (fallback to tool popup).
function buildNewContents(toolName, toolInput) {
  try {
    if (toolName === 'Write') {
      return { newContents: toolInput.content ?? null, oldContent: null };
    }
    if (toolName === 'Edit') {
      const filePath = toolInput.file_path;
      if (!filePath) return { newContents: null, oldContent: null };
      if (!path.isAbsolute(filePath) || filePath.split(path.sep).includes('..')) return { newContents: null, oldContent: null };
      const oldContent = fs.readFileSync(filePath, 'utf8');
      const oldStr = toolInput.old_string ?? '';
      const newStr = toolInput.new_string ?? '';
      if (!oldStr) return { newContents: null, oldContent: null };
      const newContents = toolInput.replace_all
        ? oldContent.replaceAll(oldStr, newStr)
        : oldContent.replace(oldStr, newStr);
      return { newContents, oldContent };
    }
  } catch { /* unreadable */ }
  return { newContents: null, oldContent: null };
}

// Attaches a close handler to a popup proc: reads choice file and sends key to Claude.
function installChoiceHandler(proc, window, choiceFile, logTag) {
  proc.on('close', () => {
    console.log(`[mcp] ${logTag} closed`);
    if (!window) return;
    try {
      const c = fs.readFileSync(choiceFile, 'utf8').trim();
      fs.unlinkSync(choiceFile);
      if (['1', '2', '3'].includes(c)) {
        setTimeout(() => {
          const paneOut = spawnSync('tmux', ['capture-pane', '-t', `claude:=${window}`, '-p'], { encoding: 'utf8' }).stdout ?? '';
          const maxOption = detectMaxOption(paneOut);
          const key = Number(c) > maxOption ? String(maxOption) : c;
          console.log(`[mcp] send-keys ${logTag}-choice=${c} key=${key} to ${window}`);
          spawnSync('tmux', ['send-keys', '-t', `claude:=${window}`, key]);
        }, 100);
      }
    } catch { /* no choice file — user closed popup without selecting */ }
  });
}

// Edit/Write の diff popup を非同期で起動（WebSocket openDiff の代替）
function triggerDiffPopupForWindow(window, toolName, toolInput, httpSocket) {
  const filePath = toolInput.file_path;
  const { newContents, oldContent } = buildNewContents(toolName, toolInput);

  if (!filePath || newContents === null) {
    console.log(`[mcp] diff fallback to tool-popup for ${toolName}`);
    httpSocket.end('HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n');
    triggerPopupForWindow(window, toolName, toolInput);
    return;
  }

  const client = findActiveClient();
  if (!client) {
    console.log('[mcp] diff popup: no active client');
    httpSocket.end('HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n');
    return;
  }

  const diffScript = path.join(__dirname, 'claude-diff.js');
  const encoded = Buffer.from(newContents, 'utf8').toString('base64');
  const safeWin = (window ?? '').replace(/[^a-zA-Z0-9_-]/g, '_');
  const choiceFile = `/tmp/tmux-claude-diff-choice-${safeWin}.txt`;

  // reuse oldContent from buildNewContents (Edit) to avoid a second file read
  const oldLines = oldContent ? oldContent.split('\n') : (() => { try { return fs.readFileSync(filePath, 'utf8').split('\n'); } catch { return []; } })();
  const newLines = newContents.split('\n');
  const diffLineCount = Math.abs(newLines.length - oldLines.length) + Math.min(newLines.length, oldLines.length);
  const maxLineLen = Math.max(
    newLines.reduce((m, l) => Math.max(m, l.length), 0),
    oldLines.reduce((m, l) => Math.max(m, l.length), 0),
    40
  );

  const dimResult = spawnSync('tmux', ['display-message', '-c', client, '-p', '#{client_width} #{client_height}'], { encoding: 'utf8' });
  const [termW, termH] = (dimResult.stdout ?? '').trim().split(' ').map(Number);
  const wPct = termW > 0 ? Math.min(95, Math.max(70, Math.round((maxLineLen + 12) / termW * 100))) : 90;
  const hPct = termH > 0 ? Math.min(95, Math.max(50, Math.round((diffLineCount + 8) / termH * 100))) : 80;

  const cwdResult = window ? spawnSync('tmux', ['display-message', '-t', `claude:=${window}`, '-p', '#{pane_current_path}'], { encoding: 'utf8' }) : null;
  const diffCwd = (cwdResult?.stdout ?? '').trim();

  console.log(`[mcp] diff popup ${toolName} file=${filePath} size=${wPct}%x${hPct}%`);

  // HTTP 200 を即返してから diff popup を非同期起動（hook timeout 対策）
  httpSocket.end('HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n');

  const proc = spawn('tmux', [
    'display-popup', '-c', client, `-w${wPct}%`, `-h${hPct}%`, '-E',
    `TMUX_CLAUDE_NEW_CONTENTS=${shellQuote(encoded)} TOOL_CWD=${shellQuote(diffCwd)} node ${shellQuote(diffScript)} ${shellQuote(filePath)} ${shellQuote(window ?? '')}`,
  ], { detached: false });
  proc.stderr.on('data', d => console.warn(`[mcp] diff popup stderr: ${d.toString().trim()}`));
  proc.on('error', e => console.warn(`[mcp] diff popup error: ${e.message}`));
  installChoiceHandler(proc, window, choiceFile, 'diff-popup');
}

// Launches the tool confirmation popup asynchronously.
// After the popup closes, reads CHOICE_FILE and sends the key to Claude.
function triggerPopupForWindow(window, toolName, toolInput) {
  const client = findActiveClient();
  if (!client) { console.warn('[mcp] no active client for popup'); return; }

  const type = getNotifyType();
  console.log(`[mcp] popup type=${type} window=${window ?? '?'} tool=${toolName || '?'} client=${client}`);

  if (type === 'menu') {
    const popupScript = path.join(__dirname, 'claude-popup.sh');
    const popupCmd = window ? `${shellQuote(popupScript)} ${shellQuote(window)}` : shellQuote(popupScript);
    spawnSync('tmux', [
      'display-menu', '-c', client,
      '-T', 'Claude: permission required',
      'Open Claude', 'o', `display-popup -c ${shellQuote(client)} -w90% -h80% -E ${popupCmd}`,
      'Dismiss',     'd', '',
    ]);
    return;
  }

  // popup path: launch display-popup asynchronously so HTTP 200 can be sent immediately
  // claude-tool-popup.js handles keyboard input itself via process.stdin.on('data', ...)
  // (same approach as claude-diff.js — display-popup provides a PTY for stdin)
  const toolPopupScript = path.join(__dirname, 'claude-tool-popup.js');
  const toolInputJson = JSON.stringify(toolInput ?? {});
  const safeWin = (window ?? '').replace(/[^a-zA-Z0-9_-]/g, '_');
  const choiceFile = `/tmp/tmux-claude-tool-choice-${safeWin}.txt`;
  const cwdResult = window ? spawnSync('tmux', ['display-message', '-t', `claude:=${window}`, '-p', '#{pane_current_path}'], { encoding: 'utf8' }) : null;
  const toolCwd = (cwdResult?.stdout ?? '').trim();
  const popupCmd = `TOOL_NAME=${shellQuote(toolName || '')} TOOL_INPUT=${shellQuote(toolInputJson)} TOOL_CWD=${shellQuote(toolCwd)} node ${shellQuote(toolPopupScript)} ${shellQuote(window ?? '')}`;

  const dimResult = spawnSync('tmux', ['display-message', '-c', client, '-p', '#{client_width} #{client_height}'], { encoding: 'utf8' });
  const [termW, termH] = (dimResult.stdout ?? '').trim().split(' ').map(Number);
  // CWD は claude-tool-popup.js 内で ~ 置換されるので近似値として使用
  const cwdDisplayLen = toolCwd ? toolCwd.replace(os.homedir(), '~').length : 0;
  const { wPct, hPct } = estimateToolPopupSize(toolName, toolInput ?? {}, termW, termH, !!toolCwd, cwdDisplayLen);
  console.log(`[mcp] popup size=${wPct}%x${hPct}% (term=${termW}x${termH})`);

  const proc = spawn('tmux', [
    'display-popup', '-c', client, `-w${wPct}%`, `-h${hPct}%`, '-E', popupCmd,
  ], { detached: false });
  proc.stderr.on('data', d => console.warn(`[mcp] display-popup stderr: ${d.toString().trim()}`));
  proc.on('error', e => console.warn(`[mcp] display-popup error: ${e.message}`));

  installChoiceHandler(proc, window, choiceFile, 'tool-popup');
}

function triggerPopup(socket) {
  const window = socketState.get(socket)?.window ?? null;
  triggerPopupForWindow(window, '', {});
}

// --- MCP message handler ---

function handleMcpMessage(socket, msg) {
  const { id, method, params } = msg;

  switch (method) {
    case 'initialize':
      reply(socket, id, {
        protocolVersion: params?.protocolVersion ?? '2025-03-26',
        capabilities: { tools: {} },
        serverInfo: { name: 'tmux-claude', version: '1.0.0' },
      });
      break;

    case 'ide_connected':
      if (params?.pid) {
        const pid = params.pid;
        const localWindow = findTmuxWindowForPid(pid)?.window ?? null;
        let remoteWindow = null;
        // Only consume pending remote window if this PID has no local tmux window
        if (!localWindow) {
          const pendingFile = '/tmp/tmux-claude-next-remote-window';
          try {
            remoteWindow = fs.readFileSync(pendingFile, 'utf8').trim() || null;
            if (remoteWindow) fs.unlinkSync(pendingFile);
          } catch { /* no pending remote window */ }
        }
        const window = localWindow ?? remoteWindow;
        socketState.set(socket, { pid, window });
        pidToWindow.set(pid, window);
        console.log(`[mcp] ide_connected pid=${pid}${localWindow ? ` local-window=${localWindow}` : ''}${remoteWindow ? ` remote-window=${remoteWindow}` : ''}`);
      }
      break;

    case 'tools/list':
      reply(socket, id, { tools: [] });
      break;

    case 'tools/call':
      if (params?.name === 'openDiff') {
        const args = params.arguments ?? {};
        const oldPath = args.old_file_path;
        const newContents = args.new_file_contents;
        const window = socketState.get(socket)?.window ?? null;
        console.log(`[mcp] openDiff called oldPath=${oldPath} window=${window ?? 'null'}`);
        if (oldPath && newContents != null && window) {
          try {
            const client = findActiveClient();
            const diffScript = path.join(__dirname, 'claude-diff.js');
            const encoded = Buffer.from(newContents, 'utf8').toString('base64');
            if (client) {
              // ターミナルサイズを取得してポップアップサイズを動的に決定
              const dimResult = spawnSync('tmux', [
                'display-message', '-c', client, '-p', '#{client_width} #{client_height}',
              ], { encoding: 'utf8' });
              const [termW, termH] = (dimResult.stdout.trim().split(' ')).map(Number);

              // diff の行数と最長行からサイズを推定
              let oldLines = [];
              try { oldLines = fs.readFileSync(oldPath, 'utf8').split('\n'); } catch (e) {
                console.warn(`[mcp] cannot read old file for size estimate: ${e.message}`);
              }
              const newLines = newContents.split('\n');
              const diffLineCount = Math.abs(newLines.length - oldLines.length) + Math.min(newLines.length, oldLines.length);
              const maxLineLen = Math.max(
                newLines.reduce((m, l) => Math.max(m, l.length), 0),
                oldLines.reduce((m, l) => Math.max(m, l.length), 0),
                40
              );

              const wPct = termW > 0 ? Math.min(95, Math.max(70, Math.round((maxLineLen + 12) / termW * 100))) : 90;
              const hPct = termH > 0 ? Math.min(95, Math.max(50, Math.round((diffLineCount + 8) / termH * 100))) : 80;

              spawnSync('tmux', [
                'display-popup', '-c', client, `-w${wPct}%`, `-h${hPct}%`, '-E',
                `TMUX_CLAUDE_NEW_CONTENTS=${shellQuote(encoded)} node ${shellQuote(diffScript)} ${shellQuote(oldPath)} ${shellQuote(window)}`,
              ]);
            }
          } catch (e) {
            console.warn('[mcp] diff popup error', e.message);
            triggerPopup(socket);
          }
        } else {
          triggerPopup(socket);
        }
      }
      if (id != null) {
        let diffReply = 'TAB_CLOSED';
        const diffWindow = socketState.get(socket)?.window ?? null;
        if (diffWindow && params?.name === 'openDiff') {
          const safeWindow = diffWindow.replace(/[^a-zA-Z0-9_-]/g, '_');
          const choiceFile = `/tmp/tmux-claude-diff-choice-${safeWindow}.txt`;
          try {
            const c = fs.readFileSync(choiceFile, 'utf8').trim();
            fs.unlinkSync(choiceFile);
            if (['1', '2', '3'].includes(c)) {
              pendingDiffChoices.set(diffWindow, c);
              if (c === '3') diffReply = 'REJECTED';
              else if (c === '2') diffReply = 'ALWAYS_ALLOW';
            }
          } catch {}
        }
        reply(socket, id, { content: [{ type: 'text', text: diffReply }] });
      }
      break;

    default:
      if (id != null) reply(socket, id, {});
      break;
  }
}

// --- Connection handler ---

const connections = new Set();

function handleConnection(socket) {
  socketState.set(socket, { pid: null });
  let upgraded = false;
  let buf = Buffer.alloc(0);

  socket.on('data', chunk => {
buf = Buffer.concat([buf, chunk]);

    if (!upgraded) {
      const end = buf.indexOf('\r\n\r\n');
      if (end === -1) return;

      const headerText = buf.slice(0, end).toString('utf8');
      buf = buf.slice(end + 4);

      const headers = {};
      for (const line of headerText.split('\r\n').slice(1)) {
        const colon = line.indexOf(': ');
        if (colon !== -1) headers[line.slice(0, colon).toLowerCase()] = line.slice(colon + 2);
      }

      if (headers['x-claude-code-ide-authorization'] !== AUTH_TOKEN) {
        console.warn('[mcp] auth failed: token mismatch');
        sendHttpError(socket, 401, 'Unauthorized');
        return;
      }

      const key = headers['sec-websocket-key'];
      if (!key) {
        // HTTP POST /notify
        const requestLine = headerText.split('\r\n')[0];
        if (requestLine.startsWith('POST /notify')) {
          const contentLength = parseInt(headers['content-length'] || '0', 10);
          const readBody = (cb) => {
            if (buf.length >= contentLength) {
              cb(buf.slice(0, contentLength));
            } else {
              socket.once('data', chunk => {
                buf = Buffer.concat([buf, chunk]);
                readBody(cb);
              });
            }
          };
          readBody(body => {
            try {
              const data = JSON.parse(body.toString('utf8'));
              console.log('[mcp] /notify raw:', JSON.stringify(data));
              const rawPid = Number(data.pid);

              if (Number.isInteger(rawPid) && rawPid > 0) {
                // PreToolUse hook からのツール情報を保存
                if (data.type === 'tool_info') {
                  const window = resolveWindow(rawPid);
                  if (window) {
                    pendingToolInfo.set(window, { tool_name: data.tool_name, tool_input: data.tool_input, ts: Date.now() });
                    console.log(`[mcp] tool_info stored window=${window} tool=${data.tool_name}`);
                  }
                  socket.end('HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n');
                  return;
                }

                const window = resolveWindow(rawPid);
                if (window) {
                  console.log(`[mcp] /notify pid=${data.pid} window=${window}`);
                  const pendingChoice = pendingDiffChoices.get(window);
                  if (pendingChoice) {
                    pendingDiffChoices.delete(window);
                    socket.end('HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n');
                    setTimeout(() => {
                      console.log(`[mcp] send-keys diff-choice=${pendingChoice} to ${window}`);
                      spawnSync('tmux', ['send-keys', '-t', `claude:=${window}`, pendingChoice]);
                    }, 50);
                    return;
                  }
                  // ツール情報を取得（pendingToolInfo → message parse の順でフォールバック）
                  const info = pendingToolInfo.get(window);
                  const fresh = info && Date.now() - info.ts < 15000;
                  const msgTool = (data.message || '').match(/\buse (\w+)$/)?.[1] || '';
                  const toolName  = (fresh ? info.tool_name  : null) || data.tool_name || msgTool || '';
                  const toolInput = (fresh ? info.tool_input : null) || data.tool_input || {};
                  if (info) pendingToolInfo.delete(window);
                  console.log(`[mcp] tool resolve: pending=${fresh ? info.tool_name : 'none'} msg=${msgTool} => ${toolName}`);
                  // Edit/Write は diff popup で処理（WebSocket 不要、tool_input から直接 diff を生成）
                  const DIFF_TOOLS = new Set(['Edit', 'Write', 'MultiEdit', 'NotebookEdit']);
                  if (DIFF_TOOLS.has(toolName)) {
                    triggerDiffPopupForWindow(window, toolName, toolInput, socket);
                    return;
                  }
                  triggerPopupForWindow(window, toolName, toolInput);
                }
              }
            } catch (e) {
              console.warn('[mcp] /notify parse error', e.message);
            }
            socket.end('HTTP/1.1 200 OK\r\nContent-Length: 0\r\n\r\n');
          });
        } else {
          sendHttpError(socket, 400, 'Bad Request');
        }
        return;
      }

      send101(socket, key);
      upgraded = true;
      connections.add(socket);
      console.log(`[mcp] client connected (total: ${connections.size})`);
    }

    while (buf.length > 0) {
      const frame = parseFrame(buf);
      if (!frame) break;
      buf = buf.slice(frame.consumed);

      switch (frame.opcode) {
        case 0x1: {
          let msg;
          try { msg = JSON.parse(frame.payload.toString('utf8')); } catch { break; }
          handleMcpMessage(socket, msg);
          break;
        }
        case 0x8: socket.destroy(); break;
        case 0x9: sendPong(socket, frame.payload); break;
      }
    }
  });

  socket.on('close', () => {
    const pid = socketState.get(socket)?.pid;
    if (pid) {
      pidToWindow.delete(pid);
    }
    connections.delete(socket);
    socketState.delete(socket);
    console.log(`[mcp] client disconnected (total: ${connections.size})`);
  });

  socket.on('error', () => {
    connections.delete(socket);
    socketState.delete(socket);
  });
}

// --- Lock file ---

function writeLockFile(port) {
  fs.mkdirSync(LOCK_DIR, { recursive: true });
  const lockPath = path.join(LOCK_DIR, `${port}.lock`);
  fs.writeFileSync(lockPath, JSON.stringify({
    pid: process.pid,
    workspaceFolders: [],
    ideName: 'tmux-claude',
    transport: 'ws',
    authToken: AUTH_TOKEN,
  }), { mode: 0o600 });
  return lockPath;
}

function deleteLockFile(port) {
  try { fs.unlinkSync(path.join(LOCK_DIR, `${port}.lock`)); } catch { /* ok */ }
}

// --- Main ---

const server = net.createServer(handleConnection);

// 環境変数でポートを固定可能（再起動後も同じポートで起動し Claude Code が自動再接続できる）
const LISTEN_PORT = parseInt(process.env.TMUX_CLAUDE_PORT || '0', 10);

server.listen(LISTEN_PORT, '127.0.0.1', () => {
  const { port } = server.address();
  const lockPath = writeLockFile(port);

  fs.writeFileSync(PID_FILE,   String(process.pid), { mode: 0o600 });
  fs.writeFileSync(PORT_FILE,  String(port),        { mode: 0o600 });
  fs.writeFileSync(TOKEN_FILE, AUTH_TOKEN,           { mode: 0o600 });
  // 既存ファイルの場合 mode が反映されないため明示的に変更
  fs.chmodSync(TOKEN_FILE, 0o600);

  console.log(`[mcp] started  port=${port}  pid=${process.pid}`);
  console.log(`[mcp] lock     ${lockPath}`);

  function cleanup() {
    deleteLockFile(port);
    try { fs.unlinkSync(PID_FILE); } catch { /* ok */ }
    try { fs.unlinkSync(PORT_FILE); } catch { /* ok */ }
    // TOKEN_FILE は削除しない — 再起動後も同じ token で Claude Code が自動再接続できる
    console.log('[mcp] stopped');
    process.exit(0);
  }

  process.on('SIGTERM', cleanup);
  process.on('SIGINT', cleanup);
});
