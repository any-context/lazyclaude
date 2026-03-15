#!/usr/bin/env node
'use strict';

// claude-tool-popup.js - permission confirmation popup for Claude Code tool use
// Responsibility: CONTENT — what to display and which choices to offer
// Args: window
// Env:  TOOL_NAME, TOOL_INPUT (JSON string), TOOL_CWD (optional)
// Writes choice ('1'|'2'|'3') to CHOICE_FILE then exits.

const fs = require('fs');
const { spawnSync } = require('child_process');
const {
  R, BOLD, DIM,
  GREEN, YELLOW, RED,
  dim, sep, headerLines, resolveCwd,
  enterAltScreen, renderActionBar,
  readChoice,
} = require('./popup-ui');

const WINDOW     = process.argv[2] ?? '';
const safeWindow = WINDOW.replace(/[^a-zA-Z0-9_-]/g, '_');
const CHOICE_FILE = `/tmp/tmux-claude-tool-choice-${safeWindow}.txt`;

const toolName  = process.env.TOOL_NAME  ?? '';
const toolInput = (() => { try { return JSON.parse(process.env.TOOL_INPUT ?? '{}'); } catch { return {}; } })();
const toolCwd   = resolveCwd();

const COLS = process.stdout.columns || Number(process.env.COLUMNS) || 80;
const ROWS = process.stdout.rows    || Number(process.env.LINES)   || 24;

// --- Content ---

// Capture actual dialog choices from Claude Code's permission dialog via capture-pane
function captureDialogOptions(window) {
  if (!window) return [];
  const result = spawnSync('tmux', ['capture-pane', '-t', `claude:=${window}`, '-p'], { encoding: 'utf8' });
  const pane = result.stdout ?? '';
  const options = [];
  for (const line of pane.split('\n')) {
    const m = line.match(/^\s*(?:[❯➜>]\s+)?(\d+)[.)]\s+(.+)/);
    if (m) {
      const text = m[2].replace(/\x1b\[[0-9;]*[a-zA-Z]/g, '').trim();
      options.push({ num: Number(m[1]), text });
    }
  }
  return options;
}

// Truncate a plain string to maxLen visible chars, appending '…' if cut
function trunc(s, maxLen) {
  if (s.length <= maxLen) return s;
  return s.slice(0, maxLen - 1) + '…';
}

// Build plain-text body lines from tool input (no ANSI — styling via popup-ui helpers only)
function buildBodyLines(name, input) {
  const maxW = Math.max(20, COLS - 6);
  const lines = [];
  switch (name) {
    case 'Bash': {
      for (const l of (input.command ?? '').split('\n').slice(0, 20)) lines.push(trunc(l, maxW));
      break;
    }
    case 'Read':
      lines.push(trunc(input.file_path ?? '', maxW));
      break;
    case 'Write':
      lines.push(trunc(input.file_path ?? '', maxW));
      if (input.content) lines.push(dim(`(${input.content.split('\n').length} lines)`));
      break;
    case 'Edit': {
      lines.push(trunc(input.file_path ?? '', maxW));
      const old = (input.old_string ?? '').split('\n').slice(0, 5);
      const nw  = (input.new_string ?? '').split('\n').slice(0, 5);
      if (old.length) lines.push(sep('old'), ...old.map(l => `  ${trunc(l, maxW - 2)}`));
      if (nw.length)  lines.push(sep('new'), ...nw.map(l => `  ${trunc(l, maxW - 2)}`));
      break;
    }
    case 'Agent':
    case 'Task': {
      for (const l of (input.prompt ?? input.description ?? '').split('\n').slice(0, 10)) lines.push(trunc(l, maxW));
      break;
    }
    default: {
      for (const [k, v] of Object.entries(input).slice(0, 8)) {
        const val = typeof v === 'string' ? v : JSON.stringify(v);
        lines.push(`${dim(k + ':')} ${trunc(val.split('\n')[0], maxW)}`);
      }
    }
  }
  return lines;
}

// Map dialog options to action bar entries (content: labels + key bindings)
function buildActions(dialogOptions) {
  const KEY_COLORS = [GREEN, YELLOW, RED];
  if (dialogOptions.length === 0) {
    // fallback when capture-pane returns nothing
    return [
      { key: 'y', label: 'Yes',         color: GREEN  },
      { key: 'a', label: 'Allow always', color: YELLOW },
      { key: 'n', label: 'No',          color: RED    },
    ];
  }
  const KEY_CHARS = dialogOptions.length <= 2 ? ['y', 'n'] : ['y', 'a', 'n'];
  return dialogOptions.map((opt, i) => ({
    key:   KEY_CHARS[i]  ?? String(opt.num),
    label: opt.text,
    color: KEY_COLORS[i] ?? DIM,
  }));
}

// --- Draw ---

// Layout:
//   ~/path/to/cwd        (dim, if available)
//   ─────────────────
//   Bash                 (bold tool name)
//       echo ...         (body lines, 4-space indent)
//   ─────────────────
//   Yes: y  | No: n  | cancel: Esc
//   ❯
function draw(bodyLines, actions) {
  const hdrLines = headerLines(COLS, toolCwd, toolName || 'Tool');
  // Overhead: header lines + action bar (sep+actions = 2 rows)
  const maxBody = Math.max(1, ROWS - hdrLines.length - 2);

  enterAltScreen();

  for (const l of hdrLines) process.stdout.write(l + '\x1b[K\n');

  for (const l of bodyLines.slice(0, maxBody)) {
    process.stdout.write(`    ${l}\x1b[K\n`);
  }

  renderActionBar(COLS, actions);
}

// --- Main ---

const dialogOptions = captureDialogOptions(WINDOW);
const bodyLines     = buildBodyLines(toolName, toolInput);
const actions       = buildActions(dialogOptions);
draw(bodyLines, actions);

(async () => {
  const maxOption = dialogOptions.length > 0 ? dialogOptions.length : 3;
  const choice = await readChoice(maxOption);

  if (WINDOW && choice !== null) {
    try {
      fs.writeFileSync(CHOICE_FILE, choice);
    } catch (e) {
      process.stderr.write(`[claude-tool-popup] failed to write choice: ${e.message}\n`);
    }
  }
  process.exit(0);
})();
