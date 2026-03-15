'use strict';

// popup-ui.js - shared UI primitives for claude popup scripts
// Responsibility: ALL design decisions (ANSI codes, layout, screen lifecycle)
// Used by: claude-diff.js, claude-tool-popup.js

// --- ANSI color/style helpers ---

const A      = (c) => `\x1b[${c}m`;
const R      = A(0);
const BOLD   = A(1);
const DIM    = A(2);
const GREEN  = A('38;2;64;160;43');
const YELLOW = A('38;2;223;142;29');
const RED    = A('38;2;192;72;72');
const CYAN   = A('38;2;23;146;153');

// Diff semantic colors (exported for buildLines in claude-diff.js)
const RESET_FG = '\x1b[39m';
const LINE_ADD = '\x1b[38;2;80;200;80m';
const LINE_DEL = '\x1b[38;2;220;90;90m';
const BG_RED   = '\x1b[48;2;61;1;0m';
const BG_GREEN = '\x1b[48;2;2;40;0m';

// Symbols
const BULLET = '\u23fa'; // ⏺
const CORNER = '\u23bf'; // ⎿

// --- Text styling helpers (for content files to apply design tokens) ---

function dim(text)  { return `${DIM}${text}${R}`; }
function bold(text) { return `${BOLD}${text}${R}`; }
function sep(label) { return `${DIM}─ ${label} ─${R}`; }

// --- CSI helper ---

// CSI シーケンスの終端バイトか判定 (0x40-0x7E)
function isCSIFinal(code) { return code >= 0x40 && code <= 0x7e; }

// --- Screen lifecycle ---

function enterAltScreen() {
  process.stdout.write('\x1b[?1049h\x1b[?25l');
}

function enterAltScreenWithMouse() {
  process.stdout.write('\x1b[?1049h\x1b[?25l\x1b[?1000h\x1b[?1006h');
}

// Restore terminal state (disable mouse tracking, show cursor, exit alternate screen)
function cleanup() {
  process.stdout.write('\x1b[?1000l\x1b[?1006l\x1b[?25h\x1b[?1049l');
}

// Resolve the Promise and restore stdin to cooked mode
function done(resolve, choice) {
  process.stdin.setRawMode(false);
  process.stdout.write('\n');
  resolve(choice);
}

// --- Layout primitives ---

// Render popup header: title line + subtitle/first-content line opener
// subtitle: if provided, renders as a full line; otherwise leaves cursor after CORNER for inline content
function renderHeader(title, subtitle = null) {
  process.stdout.write(`  ${BULLET} ${BOLD}${title}${R}\n`);
  if (subtitle !== null) {
    process.stdout.write(`  ${CYAN}${CORNER}${R}  ${subtitle}\x1b[K\n`);
  } else {
    process.stdout.write(`  ${CYAN}${CORNER}${R}  `);
  }
}

// Render body lines inside the popup.
// First line appends to current cursor position (after CORNER opener).
// Returns the number of body lines rendered.
function renderBodyLines(viewHeight, lines) {
  let bodyLines = 0;
  const maxBody = Math.max(1, viewHeight); // viewHeight already accounts for all overhead
  for (const l of lines.slice(0, maxBody)) {
    if (bodyLines === 0) {
      process.stdout.write(l + '\x1b[K\n');
    } else {
      process.stdout.write(`       ${l}\x1b[K\n`);
    }
    bodyLines++;
  }
  if (bodyLines === 0) process.stdout.write('\x1b[K\n');
  return bodyLines;
}

// Render a scrollable diff view (full screen repaint at current cursor home).
// lines: pre-built display lines (with ANSI). viewHeight: usable rows above action bar.
function renderDiffLines(viewHeight, lines, scrollPos) {
  process.stdout.write('\x1b[H');
  const visible = lines.slice(scrollPos, scrollPos + viewHeight);
  for (const l of visible) process.stdout.write(l + '\x1b[K\n');
  for (let i = visible.length; i < viewHeight; i++) process.stdout.write('\x1b[K\n');
}

// Render the action bar at the current cursor position.
// actions: [{key, label, color?}]  — caller defines content and key bindings
// hint: optional right-aligned hint string (e.g. scroll percentage)
function renderActionBar(cols, actions, hint = '') {
  const KEY_COLORS = [GREEN, YELLOW, RED];
  process.stdout.write('─'.repeat(cols) + '\x1b[K\n');

  const parts = actions.map(({ key, label, color }, i) => {
    const c = color ?? KEY_COLORS[i] ?? DIM;
    return `${DIM}${label}${R}: ${c}${BOLD}${key}${R}`;
  });
  const escHint = `${DIM}cancel${R}: ${DIM}${BOLD}Esc${R}`;

  process.stdout.write('  ' + [...parts, escHint].join(`  ${DIM}|${R}  `) + hint + '\x1b[K\n');
  process.stdout.write(`  ${BOLD}❯${R} \x1b[K`);
}

// --- Input ---

// Read a single y/a/n choice from stdin (no scroll support).
// maxOption: dialog option count (2 or 3). 'n' maps to String(maxOption).
// Returns Promise<'1'|'2'|'3'|null>  null = cancel (Esc / Ctrl-C)
function readChoice(maxOption = 3) {
  if (!process.stdin.isTTY) {
    cleanup();
    return Promise.resolve(String(maxOption)); // non-TTY → deny as safe default
  }
  return new Promise((resolve) => {
    process.stdin.setRawMode(true);
    process.stdin.resume();

    let buf = '';
    let settled = false;
    let escTimer = null;

    function finish(choice) {
      if (settled) return;
      settled = true;
      if (escTimer) { clearTimeout(escTimer); escTimer = null; }
      process.stdin.setRawMode(false);
      cleanup();
      process.stdout.write('\n');
      resolve(choice);
    }

    process.stdin.on('data', (chunk) => {
      if (escTimer) { clearTimeout(escTimer); escTimer = null; }
      buf += chunk.toString();

      while (buf.length > 0) {
        if (buf[0] === '\x1b') {
          if (buf.length === 1) {
            // Esc 単体かもしれない — 30ms 待ってシーケンス継続がなければ cancel
            escTimer = setTimeout(() => { buf = ''; finish(null); }, 30);
            break;
          }
          if (buf[1] === '[') {
            // CSI シーケンス — 終端バイトまで待つ
            let i = 2;
            while (i < buf.length && !isCSIFinal(buf.charCodeAt(i))) i++;
            if (i >= buf.length) break;
            buf = buf.slice(i + 1);
          } else {
            // Alt+key または不明シーケンス → cancel
            finish(null); return;
          }
        } else {
          const ch = buf[0];
          buf = buf.slice(1);
          if      (ch === 'y' || ch === 'Y' || ch === '1') { finish('1'); return; }
          else if (ch === 'a' || ch === 'A' || ch === '2') { finish(String(Math.min(2, maxOption))); return; }
          else if (ch === 'n' || ch === 'N' || ch === '3') { finish(String(maxOption)); return; }
          else if (ch === '\x03') { finish(null); return; } // Ctrl-C → cancel
        }
      }
    });
  });
}

module.exports = {
  // ANSI constants
  A, R, BOLD, DIM, GREEN, YELLOW, RED, CYAN,
  RESET_FG, LINE_ADD, LINE_DEL, BG_RED, BG_GREEN,
  BULLET, CORNER,
  // Text styling helpers
  dim, bold, sep,
  // CSI
  isCSIFinal,
  // Screen lifecycle
  enterAltScreen, enterAltScreenWithMouse, cleanup, done,
  // Layout
  renderHeader, renderBodyLines, renderDiffLines, renderActionBar,
  // Input
  readChoice,
};
