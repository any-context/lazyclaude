#!/usr/bin/env node
'use strict';

// claude-tool-popup.js - standalone permission confirmation popup for Claude Code tool use
// Args: window
// Env:  TOOL_NAME, TOOL_INPUT (JSON string)
// Writes choice ('1'|'2'|'3') to CHOICE_FILE for MCP server to send-keys.

const fs = require('fs');

const WINDOW     = process.argv[2] ?? '';
const safeWindow = WINDOW.replace(/[^a-zA-Z0-9_-]/g, '_');
const CHOICE_FILE = `/tmp/tmux-claude-tool-choice-${safeWindow}.txt`;

const toolName  = process.env.TOOL_NAME  ?? '';
const toolInput = (() => { try { return JSON.parse(process.env.TOOL_INPUT ?? '{}'); } catch { return {}; } })();

// ANSI helpers (matching claude-diff.js style)
const A       = (c) => `\x1b[${c}m`;
const R       = A(0);
const BOLD    = A(1);
const DIM     = A(2);
const GREEN   = A('38;2;64;160;43');
const YELLOW  = A('38;2;223;142;29');
const RED     = A('38;2;192;72;72');
const CYAN    = A('38;2;23;146;153');
const BULLET  = '\u23fa'; // ⏺
const CORNER  = '\u23bf'; // ⎿

const COLS = process.stdout.columns || Number(process.env.COLUMNS) || 80;
const ROWS = process.stdout.rows    || Number(process.env.LINES)   || 24;

// Extract the main "subject" from tool input depending on tool type
function formatToolInput(name, input) {
  const lines = [];
  switch (name) {
    case 'Bash': {
      const cmd = input.command ?? '';
      for (const l of cmd.split('\n').slice(0, 20)) lines.push(l);
      break;
    }
    case 'Read':
      lines.push(input.file_path ?? '');
      break;
    case 'Write':
      lines.push(input.file_path ?? '');
      if (input.content) lines.push(`${DIM}(${input.content.split('\n').length} lines)${R}`);
      break;
    case 'Edit': {
      lines.push(input.file_path ?? '');
      const old = (input.old_string ?? '').split('\n').slice(0, 5);
      const nw  = (input.new_string ?? '').split('\n').slice(0, 5);
      if (old.length) lines.push(`${DIM}─ old ─${R}`, ...old.map(l => `  ${l}`));
      if (nw.length)  lines.push(`${DIM}─ new ─${R}`, ...nw.map(l => `  ${l}`));
      break;
    }
    case 'Agent':
    case 'Task': {
      const prompt = input.prompt ?? input.description ?? '';
      for (const l of prompt.split('\n').slice(0, 10)) lines.push(l);
      break;
    }
    default: {
      // Generic: key: value pairs
      for (const [k, v] of Object.entries(input).slice(0, 8)) {
        const val = typeof v === 'string' ? v : JSON.stringify(v);
        lines.push(`${DIM}${k}:${R} ${val.split('\n')[0]}`);
      }
    }
  }
  return lines;
}

const inputLines = formatToolInput(toolName, toolInput);
const viewHeight = Math.max(1, ROWS - 4);

// Alternate screen + hide cursor
process.stdout.write('\x1b[?1049h\x1b[?25l');

// Header
process.stdout.write(`\n  ${BULLET} ${BOLD}${toolName || 'Tool'}${R}\n`);
process.stdout.write(`  ${CYAN}${CORNER}${R}  `);

let bodyLines = 0;
for (const l of inputLines.slice(0, viewHeight - 4)) {
  if (bodyLines === 0) {
    process.stdout.write(l + '\x1b[K\n');
  } else {
    process.stdout.write(`       ${l}\x1b[K\n`);
  }
  bodyLines++;
}
if (bodyLines === 0) process.stdout.write('\x1b[K\n');

// Fill remaining space
const used = bodyLines + 2; // header lines
for (let i = used; i < viewHeight; i++) {
  process.stdout.write('\x1b[K\n');
}

// y/a/n bar (matching claude-diff.js)
process.stdout.write('─'.repeat(COLS) + '\x1b[K\n');
process.stdout.write(`  ${GREEN}${BOLD}y${R}  Yes        ${YELLOW}${BOLD}a${R}  Allow all in session        ${RED}${BOLD}n${R}  No\x1b[K\n`);
process.stdout.write(`  ${BOLD}❯${R} \x1b[K`);

function cleanup() {
  process.stdout.write('\x1b[?25h\x1b[?1049l');
}

if (!process.stdin.isTTY) {
  // Not interactive — default deny
  cleanup();
  try { fs.writeFileSync(CHOICE_FILE, '3'); } catch {}
  process.exit(0);
}

process.stdin.setRawMode(true);
process.stdin.resume();
process.stdin.once('data', (buf) => {
  const ch = buf.toString()[0];
  process.stdin.setRawMode(false);
  cleanup();
  const choice = (ch === 'y' || ch === 'Y' || ch === '1') ? '1'
               : (ch === 'a' || ch === 'A' || ch === '2') ? '2'
               : '3';
  try {
    fs.writeFileSync(CHOICE_FILE, choice);
  } catch (e) {
    process.stderr.write(`[claude-tool-popup] failed to write choice: ${e.message}\n`);
  }
  process.exit(0);
});