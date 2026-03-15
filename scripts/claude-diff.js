#!/usr/bin/env node
'use strict';

// claude-diff.js - Claude Code format diff viewer
// Responsibility: CONTENT — diff data, line building, scroll interaction
// Args: old_file_path window
// new_file_contents passed via env: TMUX_CLAUDE_NEW_CONTENTS (base64)

const fs = require('fs');
const { spawnSync } = require('child_process');
const {
  R, BOLD, DIM, GREEN, YELLOW, RED,
  RESET_FG, LINE_ADD, LINE_DEL, BG_RED, BG_GREEN,
  headerLines, resolveCwd,
  isCSIFinal, cleanup, done,
  enterAltScreen, enterAltScreenWithMouse,
  renderDiffLines, renderActionBar,
  readChoice,
} = require('./popup-ui');

const OLD_PATH  = process.argv[2];
const WINDOW    = process.argv[3];
const toolCwd   = resolveCwd();
const newContents = Buffer.from(process.env.TMUX_CLAUDE_NEW_CONTENTS ?? '', 'base64').toString('utf8');

// Write new contents to temp
const tmpNew = `/tmp/tmux-claude-diff-${Date.now()}.tmp`;
fs.writeFileSync(tmpNew, newContents);

// Run git diff
let diffOutput = '';
try {
  const r = spawnSync('git', ['diff', '--unified=3', '--no-index', '--', OLD_PATH, tmpNew], { encoding: 'utf8' });
  diffOutput = r.stdout ?? '';
} catch { /* git not available */ }

// Get bat syntax-highlighted lines
function getHighlightedLines(filePath, displayPath) {
  const args = ['--color=always', '--plain', '--paging=never'];
  if (displayPath && displayPath !== filePath) args.push('--file-name', displayPath);
  args.push(filePath);
  try {
    const r = spawnSync('bat', args, { encoding: 'utf8' });
    if (r.status === 0 && r.stdout) return r.stdout.split('\n');
  } catch {}
  try { return fs.readFileSync(filePath, 'utf8').split('\n'); } catch { return []; }
}

// Parse unified diff hunks
function parseDiff(text) {
  const hunks = [];
  let hunk = null;
  for (const line of text.split('\n')) {
    const m = line.match(/^@@ -(\d+)(?:,\d+)? \+(\d+)(?:,\d+)? @@/);
    if (m) { hunk = { oldStart: +m[1], newStart: +m[2], lines: [] }; hunks.push(hunk); }
    else if (hunk && /^[ \-+]/.test(line)) hunk.lines.push(line);
  }
  return hunks;
}

function stripAnsi(s) { return s.replace(/\x1b\[[0-9;]*m/g, ''); }

// Build display lines for the diff viewer (content with semantic colors from popup-ui)
function buildLines(hunks, oldHighlighted, newHighlighted, cols) {
  let added = 0, removed = 0;
  for (const h of hunks)
    for (const l of h.lines) { if (l[0] === '+') added++; if (l[0] === '-') removed++; }

  const subtitle = `Added ${added} line${added !== 1 ? 's' : ''}, removed ${removed} line${removed !== 1 ? 's' : ''}`;
  const out = [...headerLines(cols, toolCwd, `Update(${OLD_PATH})`, subtitle), ''];

  const PAD = 6;

  for (const h of hunks) {
    let oldLine = h.oldStart, newLine = h.newStart, lastRemoved = null;

    for (const line of h.lines) {
      const type = line[0];
      const oldContent = (oldHighlighted[oldLine - 1] ?? line.slice(1)).replace(/\r?\n$/, '');
      const newContent = (newHighlighted[newLine - 1] ?? line.slice(1)).replace(/\r?\n$/, '');

      if (type === ' ') {
        out.push(`${DIM}${String(oldLine).padStart(PAD)}${R}  ${oldContent}`);
        oldLine++; newLine++; lastRemoved = null;
      } else if (type === '-') {
        const raw = stripAnsi(oldContent);
        const fill = ' '.repeat(Math.max(0, cols - PAD - 2 - raw.length));
        out.push(`${BG_RED}${LINE_DEL}${String(oldLine).padStart(PAD)} -${RESET_FG}${raw}${fill}${R}`);
        lastRemoved = oldLine; oldLine++;
      } else if (type === '+') {
        const highlighted = newContent.replace(/\x1b\[0m/g, `\x1b[0m${BG_GREEN}`);
        const visibleLen = stripAnsi(newContent).length;
        const fill = ' '.repeat(Math.max(0, cols - PAD - 2 - visibleLen));
        const num = lastRemoved ?? newLine;
        out.push(`${BG_GREEN}${LINE_ADD}${String(num).padStart(PAD)} +${RESET_FG}${highlighted}${fill}${R}`);
        lastRemoved = null; newLine++;
      }
    }
    out.push('');
  }
  return out;
}

// Action bar definition for diff (content: fixed 3-option diff choices)
const DIFF_ACTIONS = [
  { key: 'y', label: 'Yes',                  color: GREEN  },
  { key: 'a', label: 'Allow all in session', color: YELLOW },
  { key: 'n', label: 'No',                   color: RED    },
];

// Interactive scrollable viewer — action bar always visible at bottom
function interactiveView(lines, cols, rows) {
  const viewHeight = Math.max(1, rows - 4);
  const halfPage   = Math.max(1, Math.floor(viewHeight / 2));
  const maxScroll  = Math.max(0, lines.length - viewHeight);
  let scrollPos    = 0;
  let lastKey      = null; // gg 検出用

  enterAltScreenWithMouse();

  function draw() {
    renderDiffLines(viewHeight, lines, scrollPos);
    const pct = maxScroll > 0 ? `  ${DIM}${Math.round(scrollPos / maxScroll * 100)}%${R}` : '';
    renderActionBar(cols, DIFF_ACTIONS, pct);
  }

  function scroll(delta) {
    scrollPos = Math.max(0, Math.min(maxScroll, scrollPos + delta));
    draw();
  }

  draw();

  return new Promise((resolve) => {
    process.stdin.setRawMode(true);
    process.stdin.resume();

    let buf = '';
    let escTimer = null;

    function finish(choice) {
      if (escTimer) { clearTimeout(escTimer); escTimer = null; }
      cleanup();
      done(resolve, choice);
    }

    process.stdin.on('data', (chunk) => {
      if (escTimer) { clearTimeout(escTimer); escTimer = null; }
      buf += chunk.toString();

      while (buf.length > 0) {
        if (buf[0] === '\x1b') {
          if (buf.length === 1) {
            escTimer = setTimeout(() => { buf = ''; finish(null); }, 30);
            break;
          }

          if (buf[1] === '[') {
            // CSI シーケンス — 終端バイトまで読む
            let i = 2;
            while (i < buf.length && !isCSIFinal(buf.charCodeAt(i))) i++;
            if (i >= buf.length) break;
            const seq = buf.slice(0, i + 1);
            buf = buf.slice(i + 1);

            if      (seq === '\x1b[A')  { scroll(-1); }
            else if (seq === '\x1b[B')  { scroll(1); }
            else if (seq === '\x1b[5~') { scroll(-viewHeight); }
            else if (seq === '\x1b[6~') { scroll(viewHeight); }
            else if (seq.startsWith('\x1b[<')) {
              // SGR マウスイベント
              const btn = parseInt(seq.slice(3));
              if      (btn === 64) { scroll(-3); } // ホイール上
              else if (btn === 65) { scroll(3);  } // ホイール下
            }
            lastKey = null;
          } else {
            // Alt+key または不明 → cancel
            finish(null); return;
          }
        } else {
          const ch = buf[0];
          buf = buf.slice(1);

          if      (ch === 'y' || ch === 'Y' || ch === '1') { finish('1'); return; }
          else if (ch === 'a' || ch === 'A' || ch === '2') { finish('2'); return; }
          else if (ch === 'n' || ch === 'N' || ch === '3') { finish('3'); return; }
          else if (ch === '\x03') { finish(null); return; } // Ctrl-C → cancel
          else if (ch === 'j' || ch === '\x0e') { scroll(1); lastKey = ch; }
          else if (ch === 'k' || ch === '\x10') { scroll(-1); lastKey = ch; }
          else if (ch === 'd' || ch === '\x04') { scroll(halfPage); lastKey = ch; }
          else if (ch === 'u' || ch === '\x15') { scroll(-halfPage); lastKey = ch; }
          else if (ch === 'f' || ch === '\x06') { scroll(viewHeight); lastKey = ch; }
          else if (ch === 'b' || ch === '\x02') { scroll(-viewHeight); lastKey = ch; }
          else if (ch === 'G') { scrollPos = maxScroll; draw(); lastKey = 'G'; }
          else if (ch === 'g') {
            if (lastKey === 'g') { scrollPos = 0; draw(); lastKey = null; }
            else { lastKey = 'g'; }
          }
          else { lastKey = ch; }
        }
      }
    });
  });
}

// --- Main ---
const COLS = process.stdout.columns || Number(process.env.COLUMNS) || 80;
const ROWS = process.stdout.rows    || Number(process.env.LINES)   || 24;

const oldHighlighted = getHighlightedLines(OLD_PATH, OLD_PATH);
const newHighlighted = getHighlightedLines(tmpNew, OLD_PATH);
try { fs.unlinkSync(tmpNew); } catch { /* ok */ }

const hunks = parseDiff(diffOutput);

(async () => {
  let choice;
  if (hunks.length === 0) {
    enterAltScreen();
    process.stdout.write(`${DIM}(no changes)${R}\n`);
    renderActionBar(COLS, DIFF_ACTIONS);
    choice = await readChoice();
  } else {
    const diffLines = buildLines(hunks, oldHighlighted, newHighlighted, COLS);
    choice = await interactiveView(diffLines, COLS, ROWS);
  }

  if (WINDOW && choice !== null) {
    const safeWindow = WINDOW.replace(/[^a-zA-Z0-9_-]/g, '_');
    try {
      fs.writeFileSync(`/tmp/tmux-claude-diff-choice-${safeWindow}.txt`, choice);
    } catch (e) {
      process.stderr.write(`[claude-diff] failed to write choice: ${e.message}\n`);
    }
  }
  process.exit(0);
})();
