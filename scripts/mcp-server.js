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
const { execSync, spawnSync } = require('node:child_process');

// --- constants ---

const LOCK_DIR = path.join(os.homedir(), '.claude', 'ide');
const PID_FILE = '/tmp/tmux-claude-mcp.pid';
const PORT_FILE = '/tmp/tmux-claude-mcp.port';
const TOKEN_FILE = '/tmp/tmux-claude-mcp.token';
const WS_MAGIC = '258EAFA5-E914-47DA-95CA-C5AB0DC85B11';

const AUTH_TOKEN = process.env.TMUX_CLAUDE_TOKEN || crypto.randomUUID();

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
    const out = execSync('tmux list-clients -F "#{client_name} #{client_session} #{client_activity}"', { encoding: 'utf8' });
    const clients = out.trim().split('\n')
      .filter(Boolean)
      .map(l => { const [name, sess, activity] = l.split(' '); return { name, sess, activity: Number(activity) }; })
      .sort((a, b) => b.activity - a.activity);
    // Prefer non-claude client, fall back to any client
    return (clients.find(c => c.sess !== 'claude') ?? clients[0])?.name ?? null;
  } catch { return null; }
}

function findTmuxWindowForPid(pid) {
  const paneMap = new Map();
  try {
    for (const line of execSync('tmux list-panes -a -F "#{pane_pid} #{session_name} #{window_name}"', { encoding: 'utf8' }).trim().split('\n').filter(Boolean)) {
      const [panePid, session, window] = line.split(' ');
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

function getNotifyType() {
  try {
    const val = execSync('tmux show-option -gv @claude-notify-type 2>/dev/null', { encoding: 'utf8' }).trim();
    return val === 'menu' ? 'menu' : 'popup';
  } catch { return 'popup'; }
}

function triggerPopupForWindow(window) {
  const client = findActiveClient();
  if (!client) { console.warn('[mcp] no active client for popup'); return; }

  const popupScript = path.join(__dirname, 'claude-popup.sh');
  const popupCmd = window ? `${popupScript} ${window}` : popupScript;
  const type = getNotifyType();
  console.log(`[mcp] popup type=${type} window=${window ?? '?'} client=${client}`);

  if (type === 'menu') {
    spawnSync('tmux', [
      'display-menu', '-c', client,
      '-T', 'Claude: file written',
      'Open Claude', 'o', `display-popup -c ${client} -w80% -h80% -E '${popupCmd}'`,
      'Dismiss',     'd', '',
    ]);
  } else {
    spawnSync('tmux', ['display-popup', '-c', client, '-w80%', '-h80%', '-E', popupCmd]);
  }
}

function triggerPopup(socket) {
  const window = socketState.get(socket)?.window ?? null;
  triggerPopupForWindow(window);
}

// --- MCP message handler ---

function handleMcpMessage(socket, msg) {
  console.log('[mcp] <<', JSON.stringify(msg));
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
        console.log('[mcp] openDiff → triggering popup');
        triggerPopup(socket);
      }
      if (id != null) reply(socket, id, { content: [{ type: 'text', text: '{}' }] });
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
        console.warn(`[mcp] auth failed: got="${headers['x-claude-code-ide-authorization']}" expected="${AUTH_TOKEN}"`);
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
              if (data.pid) {
                let current = Number(data.pid);
                for (let i = 0; i < 15; i++) {
                  if (pidToWindow.has(current)) {
                    const window = pidToWindow.get(current);
                    console.log(`[mcp] /notify pid=${data.pid} matched=${current} window=${window ?? '?'}`);
                    triggerPopupForWindow(window);
                    break;
                  }
                  try {
                    const ppid = execSync(`ps -o ppid= -p ${current} 2>/dev/null`, { encoding: 'utf8' }).trim();
                    if (!ppid || ppid === '1' || ppid === '0' || ppid === String(current)) break;
                    current = Number(ppid);
                  } catch { break; }
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
  }));
  return lockPath;
}

function deleteLockFile(port) {
  try { fs.unlinkSync(path.join(LOCK_DIR, `${port}.lock`)); } catch { /* ok */ }
}

// --- Main ---

const server = net.createServer(handleConnection);

server.listen(0, '127.0.0.1', () => {
  const { port } = server.address();
  const lockPath = writeLockFile(port);

  fs.writeFileSync(PID_FILE, String(process.pid));
  fs.writeFileSync(PORT_FILE, String(port));
  fs.writeFileSync(TOKEN_FILE, AUTH_TOKEN);

  console.log(`[mcp] started  port=${port}  pid=${process.pid}`);
  console.log(`[mcp] lock     ${lockPath}`);

  function cleanup() {
    deleteLockFile(port);
    try { fs.unlinkSync(PID_FILE); } catch { /* ok */ }
    try { fs.unlinkSync(PORT_FILE); } catch { /* ok */ }
    try { fs.unlinkSync(TOKEN_FILE); } catch { /* ok */ }
    console.log('[mcp] stopped');
    process.exit(0);
  }

  process.on('SIGTERM', cleanup);
  process.on('SIGINT', cleanup);
});
