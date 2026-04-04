package session

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// ScriptConfig holds all parameters needed to generate a Claude Code launch script.
// Used by both local and SSH session types to produce a bash script.
type ScriptConfig struct {
	// SessionID is the unique session identifier (used for temp file naming).
	SessionID string

	// WorkDir is the directory to cd into before launching Claude.
	// Empty or "." means skip cd. For SSH, this is a remote path.
	WorkDir string

	// Flags are additional claude CLI flags (e.g. --resume).
	// Do not include --settings or --append-system-prompt here.
	Flags []string

	// MCP holds MCP server info for SSH lock file setup.
	// Nil for local sessions (hooks discover via existing lock files).
	MCP *MCPConfig

	// HooksJSON is the hooks settings JSON content to embed in the script.
	// Empty means skip hooks injection.
	HooksJSON string

	// SystemPrompt is injected via --append-system-prompt.
	// Empty means skip.
	SystemPrompt string

	// UserPrompt is passed as a positional argument to claude.
	// Empty means skip.
	UserPrompt string

	// WindowName is the tmux window name (e.g. "lc-abcdef01").
	// Exported as _LC_WINDOW so hooks can identify their window directly.
	// Required when MCP is set; empty for local sessions.
	WindowName string

	// SelfDelete causes the script to rm -f "$0" at startup.
	// Used for local temp scripts that should clean up after execution.
	SelfDelete bool
}

// MCPConfig holds MCP server connection info for SSH sessions.
type MCPConfig struct {
	Port  int
	Token string
}

// BuildScript generates bash script content for launching Claude Code.
// Handles both local and SSH contexts via ScriptConfig flags.
//
// The generated script follows a strict section order:
//  1. Shebang
//  2. Self-delete (if SelfDelete)
//  3. MCP lock file setup + lazyclaude shell function (if MCP != nil)
//  4. cd WorkDir
//  5. Hooks settings file (if HooksJSON non-empty)
//  6. System/user prompt variables (base64-encoded)
//  7. Auth environment variables
//  8. exec claude via login shell
func BuildScript(cfg ScriptConfig) (string, error) {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")

	// 1. Self-delete
	if cfg.SelfDelete {
		b.WriteString("rm -f \"$0\"\n")
	}

	// 2. MCP lock file setup
	if cfg.MCP != nil {
		if err := writeMCPSetup(&b, cfg.MCP, cfg.WindowName); err != nil {
			return "", err
		}
	}

	// 3. cd WorkDir
	if cfg.WorkDir != "" && cfg.WorkDir != "." {
		b.WriteString(fmt.Sprintf("cd %s\n", posixQuote(cfg.WorkDir)))
	}

	// 4. Hooks settings file
	hooksPath := ""
	if cfg.HooksJSON != "" {
		p := "/tmp/lazyclaude/hooks-settings.json"
		b.WriteString("mkdir -p /tmp/lazyclaude\n")
		b.WriteString(fmt.Sprintf("cat > '%s' << 'HOOKSEOF'\n", p))
		b.WriteString(cfg.HooksJSON + "\n")
		b.WriteString("HOOKSEOF\n")
		hooksPath = p
	}

	// 5. System prompt and user prompt via temp files (avoids all quoting issues).
	// Prompts are base64-decoded into temp files. The exec line reads them via
	// $(cat file), so shell metacharacters in prompts (backticks, $, ", etc.)
	// never appear in the command string.
	sysPromptFile := ""
	if cfg.SystemPrompt != "" {
		sysPromptFile = "/tmp/lazyclaude/sysprompt-$$.txt"
		encoded := base64.StdEncoding.EncodeToString([]byte(cfg.SystemPrompt))
		b.WriteString(fmt.Sprintf("echo %s | base64 -d > %s\n", encoded, sysPromptFile))
	}

	userPromptFile := ""
	if strings.TrimSpace(cfg.UserPrompt) != "" {
		userPromptFile = "/tmp/lazyclaude/userprompt-$$.txt"
		encoded := base64.StdEncoding.EncodeToString([]byte(cfg.UserPrompt))
		b.WriteString(fmt.Sprintf("echo %s | base64 -d > %s\n", encoded, userPromptFile))
	}

	// 6. Auth environment variables
	writeAuthEnv(&b)

	// 7. Build the claude command and exec line.
	// Always uses single quotes — prompt content is read from files at runtime.
	claudeCmd := buildClaudeCmd(cfg.Flags, hooksPath, sysPromptFile, userPromptFile)
	b.WriteString(fmt.Sprintf("exec \"$SHELL\" -lic 'exec %s'\n", claudeCmd))

	return b.String(), nil
}

// writeMCPSetup writes the MCP lock file creation and cleanup trap,
// and installs the lazyclaude executable script to PATH.
func writeMCPSetup(b *strings.Builder, mcp *MCPConfig, windowName string) error {
	lockDir := "$HOME/.claude/ide"
	lockFile := fmt.Sprintf("%s/%d.lock", lockDir, mcp.Port)

	lockContent := struct {
		PID       int    `json:"pid"`
		AuthToken string `json:"authToken"`
		Transport string `json:"transport"`
	}{PID: 0, AuthToken: mcp.Token, Transport: "ws"}
	lockJSON, err := json.Marshal(lockContent)
	if err != nil {
		return fmt.Errorf("marshal lock content: %w", err)
	}

	b.WriteString(fmt.Sprintf("mkdir -p \"%s\"\n", lockDir))
	b.WriteString(fmt.Sprintf("cat > \"%s\" << 'LOCKEOF'\n", lockFile))
	b.WriteString(string(lockJSON) + "\n")
	b.WriteString("LOCKEOF\n")
	b.WriteString(fmt.Sprintf("trap 'rm -f \"%s\"' EXIT\n", lockFile))

	// Export window name so hooks can identify their tmux window directly.
	if windowName != "" {
		b.WriteString(fmt.Sprintf("export _LC_WINDOW=%s\n", posixQuote(windowName)))
	}
	// Install lazyclaude as an executable script in PATH.
	writeLazyClaude(b, mcp.Port, mcp.Token)
	return nil
}

// writeAuthEnv writes CLAUDE_CODE_AUTO_CONNECT_IDE and passthrough auth tokens.
func writeAuthEnv(b *strings.Builder) {
	b.WriteString("export CLAUDE_CODE_AUTO_CONNECT_IDE=true\n")
	for _, key := range []string{"CLAUDE_CODE_OAUTH_TOKEN", "ANTHROPIC_API_KEY", "CLAUDE_CODE_API_KEY"} {
		if val := os.Getenv(key); val != "" {
			b.WriteString(fmt.Sprintf("export %s=%s\n", key, posixQuote(val)))
		}
	}
}

// buildClaudeCmd constructs the claude command string for the -lic argument.
//
// The exec line always uses single quotes: exec "$SHELL" -lic 'exec claude ...'.
// Prompt content is read from temp files via $(cat file) inside the login shell,
// so shell metacharacters in prompts never appear in the command string.
func buildClaudeCmd(flags []string, hooksPath, sysPromptFile, userPromptFile string) string {
	var parts []string
	parts = append(parts, "claude")

	if hooksPath != "" {
		parts = append(parts, "--settings", hooksPath)
	}

	for _, f := range flags {
		parts = append(parts, f)
	}

	if sysPromptFile != "" {
		parts = append(parts, "--append-system-prompt", fmt.Sprintf(`"$(cat %s)"`, sysPromptFile))
	}

	if userPromptFile != "" {
		parts = append(parts, fmt.Sprintf(`"$(cat %s)"`, userPromptFile))
	}

	return strings.Join(parts, " ")
}

// lazyClaudeShellFunc returns a bash snippet defining a lazyclaude() shell
// function for SSH remote sessions. Wraps curl calls to the MCP server so
// "lazyclaude msg send", "lazyclaude msg create", and "lazyclaude sessions"
// work without the binary. All fields are escaped via _lc_json_esc.
func lazyClaudeShellFunc() string {
	return `_lc_json_esc() {
  printf '%s' "$1" | sed -e 's/\\/\\\\/g' -e 's/"/\\"/g' -e $'s/\r/\\\\r/g' -e $'s/\t/\\\\t/g' | awk '{if(NR>1)printf "\\n";printf "%s",$0}'
}
lazyclaude() {
  local _lc_base="http://127.0.0.1:${_LC_MCP_PORT}"
  local _lc_auth="X-Claude-Code-Ide-Authorization: ${_LC_MCP_TOKEN}"
  case "$1" in
    msg)
      shift
      case "$1" in
        send)
          shift
          local _lc_from="cli" _lc_type="status"
          while [ $# -gt 0 ]; do
            case "$1" in
              --from) _lc_from="$2"; shift 2;;
              --type) _lc_type="$2"; shift 2;;
              *) break;;
            esac
          done
          if [ $# -lt 2 ]; then
            echo "usage: lazyclaude msg send [--from ID] [--type TYPE] <to> <body...>" >&2
            return 1
          fi
          local _lc_to="$1"; shift
          _lc_from=$(_lc_json_esc "${_lc_from}")
          _lc_to=$(_lc_json_esc "${_lc_to}")
          _lc_type=$(_lc_json_esc "${_lc_type}")
          local _lc_body
          _lc_body=$(_lc_json_esc "$*")
          curl -sf -X POST "${_lc_base}/msg/send" \
            -H "Content-Type: application/json" \
            -H "${_lc_auth}" \
            -d "{\"from\":\"${_lc_from}\",\"to\":\"${_lc_to}\",\"type\":\"${_lc_type}\",\"body\":\"${_lc_body}\"}"
          echo
          ;;
        create)
          shift
          local _lc_from="cli" _lc_name="" _lc_type="worker" _lc_prompt=""
          while [ $# -gt 0 ]; do
            case "$1" in
              --from) _lc_from="$2"; shift 2;;
              --name) _lc_name="$2"; shift 2;;
              --type) _lc_type="$2"; shift 2;;
              --prompt) _lc_prompt="$2"; shift 2;;
              *) echo "lazyclaude msg create: unknown option '$1'" >&2; return 1;;
            esac
          done
          if [ -z "${_lc_name}" ]; then
            echo "usage: lazyclaude msg create --name NAME [--from ID] [--type TYPE] [--prompt TEXT]" >&2
            return 1
          fi
          _lc_from=$(_lc_json_esc "${_lc_from}")
          _lc_name=$(_lc_json_esc "${_lc_name}")
          _lc_type=$(_lc_json_esc "${_lc_type}")
          local _lc_esc_prompt
          _lc_esc_prompt=$(_lc_json_esc "${_lc_prompt}")
          curl -sf -X POST "${_lc_base}/msg/create" \
            -H "Content-Type: application/json" \
            -H "${_lc_auth}" \
            -d "{\"from\":\"${_lc_from}\",\"name\":\"${_lc_name}\",\"type\":\"${_lc_type}\",\"prompt\":\"${_lc_esc_prompt}\"}"
          echo
          ;;
        *) echo "lazyclaude msg: unknown subcommand '$1'" >&2; return 1;;
      esac
      ;;
    sessions)
      curl -sf "${_lc_base}/msg/sessions" \
        -H "${_lc_auth}"
      echo
      ;;
    *) echo "lazyclaude: unknown command '$1'" >&2; return 1;;
  esac
}
`
}

// writeLazyClaude writes an executable lazyclaude script to /tmp/lazyclaude/bin/
// and prepends it to PATH. This makes the lazyclaude command available in any
// shell (bash, zsh, etc.) that Claude Code's Bash tool may use.
func writeLazyClaude(b *strings.Builder, port int, token string) {
	binDir := "/tmp/lazyclaude/bin"
	binPath := binDir + "/lazyclaude"
	b.WriteString(fmt.Sprintf("mkdir -p '%s'\n", binDir))
	b.WriteString(fmt.Sprintf("cat > '%s' << 'LCBINEOF'\n", binPath))
	b.WriteString("#!/bin/bash\n")
	b.WriteString(fmt.Sprintf("export _LC_MCP_PORT=%d\n", port))
	b.WriteString(fmt.Sprintf("export _LC_MCP_TOKEN=%s\n", posixQuote(token)))
	b.WriteString(lazyClaudeShellFunc())
	b.WriteString("lazyclaude \"$@\"\n")
	b.WriteString("LCBINEOF\n")
	b.WriteString(fmt.Sprintf("chmod +x '%s'\n", binPath))
	b.WriteString(fmt.Sprintf("export PATH=\"%s:$PATH\"\n", binDir))
}
