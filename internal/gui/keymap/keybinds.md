# lazyclaude Keybindings

## show_keybind_help

Open the keybind help overlay (Telescope style).
Shows all keybindings for the current mode with fuzzy search.
Type to filter, `j`/`k` to navigate, `Esc` to close.

## quit

Quit lazyclaude and terminate all managed tmux sessions.
Multiple keys are available: `q`, `Ctrl+C`, or `Ctrl+\`.

## focus_panel

Switch focus between panels (Sessions, Plugins, Logs).
`Tab` moves to the next panel, `Shift+Tab` moves to the previous.

## unsuspend_popups

Bring back suspended notification popups.
When a tool notification is dismissed with `Esc`, it is suspended rather than discarded.
Press `p` to show all suspended notifications again.

## panel_tab

Switch between tabs within the current panel.
`]` moves to the next tab, `[` moves to the previous tab.
For the Plugins panel, tabs include MCP, Plugins, and Marketplace.

## cursor_move

Navigate through list items with `j`/`k` or arrow keys.
Standard vim-style navigation used across all panels.

## collapse_expand

Collapse or expand a project group in the session tree.
`h` (or left arrow) collapses, `l` (or right arrow) expands.
When collapsed, only the project name is shown.

## new_session

Create a new Claude Code session.
`n` creates a session in the project directory.
`N` creates a session in the current working directory.
The session runs inside a lazyclaude-managed tmux window.

## delete_session

Delete the selected Claude Code session.
The tmux window associated with the session is also killed.
A confirmation prompt appears before deletion.

## attach_session

Attach to the selected session's tmux pane in full-screen mode.
All keyboard input is forwarded directly to Claude Code.
Use `Ctrl+\` to detach and return to the session list.

## launch_lazygit

Launch lazygit for the project of the currently selected session.
Opens in a new tmux window within the lazyclaude server.

## enter_fullscreen

Enter full-screen mode for the selected session.
`Enter` or `r` switches to a full-screen view where
the Claude Code output fills the entire terminal.
All keys except `Ctrl+\` and `Ctrl+D` are forwarded to Claude Code.

## start_rename

Rename the selected session.
Opens an inline input field where you can type a new name.
Press `Enter` to confirm or `Esc` to cancel.

## worktree

Manage worktree sessions for parallel Claude Code development.
`w` creates a new worktree with a dedicated branch and prompt.
`W` opens a chooser to select an existing worktree or create a new one.
Worktrees are isolated git worktrees managed by lazyclaude.

## pm_session

Start a PM (Project Manager) orchestration session.
The PM session coordinates multiple Claude Code workers,
dispatching tasks and reviewing results via the MCP server.

## send_key

Send a quick response to the selected session.
`1` sends accept (Ctrl+Y), `2` sends allow (Ctrl+A), `3` sends reject (Ctrl+N).
Useful for responding to tool permission prompts without attaching.

## purge_orphans

Remove orphaned sessions that no longer have a running tmux window.
This cleans up stale session entries from the session list.

## mcp_toggle

Toggle an MCP server between enabled and disabled.
Disabled servers are added to a deny list and will not be started
by Claude Code sessions in the current project.

## mcp_refresh

Refresh the MCP server list by re-reading the project configuration.
Useful after manually editing MCP server settings.

## plugin_toggle

Toggle a plugin between enabled and disabled states.
Disabled plugins are not loaded by lazyclaude.

## plugin_uninstall

Uninstall the selected plugin and remove its files.

## plugin_update

Update the selected plugin to the latest version.

## plugin_refresh

Refresh the plugin or marketplace list.

## plugin_install

Install the selected plugin from the marketplace.
The plugin source is cloned and registered in the lazyclaude configuration.

## logs_jump

Jump to the beginning or end of the log view.
`G` jumps to the end (most recent), `g` jumps to the top (oldest).

## logs_select

Toggle line selection in the log view.
`v` starts or extends a visual selection, similar to vim visual mode.
Selected lines can then be copied with `y`.

## logs_copy

Copy selected log lines to the system clipboard.
If no selection is active, copies the current line.

## popup_accept

Accept the tool execution request shown in the notification popup.
`Ctrl+Y` or `1` sends acceptance to the Claude Code session.

## popup_allow

Allow the tool for the remainder of this Claude Code session.
`Ctrl+A` or `2` grants permission without future prompts for this tool type.

## popup_reject

Reject the tool execution request.
`Ctrl+N` or `3` denies the tool from running.

## popup_accept_all

Accept all pending tool notifications at once.
`Y` processes all visible notification popups with acceptance.

## popup_suspend

Hide the notification popup without responding.
The popup is suspended and can be restored later with `p`.

## popup_navigate

Navigate between multiple notification popups.
Arrow keys move focus between stacked notifications.

## popup_scroll

Scroll the content of a notification popup.
`j` scrolls down, `k` scrolls up. Useful for long diff previews.

## exit_fullscreen

Exit full-screen mode and return to the session list.
`Ctrl+\` or `Ctrl+D` detaches from the Claude Code session.

## fullscreen_forward

Keys forwarded directly to the Claude Code session in full-screen mode.
Enter, Esc, and arrow keys are passed through to Claude Code.
All other keys (including typed text) are also forwarded via the input editor.

## search

Filter the current panel's items by name.
`/` opens an inline search bar at the bottom of the active panel.
Type to filter items in real time (case-insensitive substring match).
`Enter` confirms the filter and keeps the filtered view.
`Esc` cancels the search and restores the original list.
