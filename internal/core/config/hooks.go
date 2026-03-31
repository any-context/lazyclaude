package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// hookMarker identifies lazyclaude hooks. Commands containing this are ours.
const hookMarker = "/notify"

// hookVersionMarker identifies the current hook version. Old hooks that lack
// PID liveness checking don't contain this string and must be upgraded.
const hookVersionMarker = "process.kill"

// findAliveLockJS is shared JavaScript that reads all lock files, validates PID
// liveness via process.kill(pid, 0), and picks the highest port (most recent).
// Sets `best` to {lock, port} or null if no alive server found.
const findAliveLockJS = `const fs=require('fs'),path=require('path'),home=require('os').homedir();` +
	`const lockDir=path.join(home,'.claude','ide');` +
	`const locks=fs.readdirSync(lockDir).filter(f=>f.endsWith('.lock'));` +
	`let best=null;for(const f of locks){try{` +
	`const lk=JSON.parse(fs.readFileSync(path.join(lockDir,f),'utf8'));` +
	`const p=parseInt(f,10);` +
	`try{process.kill(lk.pid,0);if(!best||p>best.port)best={lock:lk,port:p};}catch{}` +
	`}catch{}}`

// resolveServerJS resolves the server port and auth token.
// Uses LAZYCLAUDE_SERVER_PORT/TOKEN env vars when available (fast path),
// falls back to lock file scanning when env vars are not set (e.g. server restart).
// Sets `srvPort` (number) and `srvToken` (string), or both null if no server found.
const resolveServerJS = `let srvPort=null,srvToken=null;` +
	`const ep=process.env.LAZYCLAUDE_SERVER_PORT,et=process.env.LAZYCLAUDE_SERVER_TOKEN;` +
	`if(ep&&et){srvPort=parseInt(ep,10);srvToken=et;}else{` + findAliveLockJS + `if(best){srvPort=best.port;srvToken=best.lock.authToken;}}`

// preToolUseHookCommand is the node one-liner for PreToolUse hooks.
// Uses env vars for fast server resolution, falls back to lock file scanning.
const preToolUseHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + resolveServerJS + `if(!srvPort){console.log(d);return;}const body=JSON.stringify({type:'tool_info',pid:process.ppid,tool_name:i.tool_name||'',tool_input:i.tool_input||{}});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/notify',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}console.log(d);})"` //nolint:lll

// notificationHookCommand is the node one-liner for Notification hooks.
// Uses env vars for fast server resolution, falls back to lock file scanning.
const notificationHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);if(i.notification_type!=='permission_prompt')return;const http=require('http');` + resolveServerJS + `if(!srvPort)return;const body=JSON.stringify({pid:process.ppid,tool_name:i.tool_name||'',tool_input:i.tool_input||{},message:i.message||''});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/notify',method:'POST',timeout:2000,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

// stopHookCommand is the node one-liner for Stop hooks.
// Fires when a Claude Code turn completes. Posts session stop event to the server.
const stopHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + resolveServerJS + `if(!srvPort)return;const body=JSON.stringify({pid:process.ppid,stop_reason:i.stop_reason||'',session_id:i.session_id||''});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/stop',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

// sessionStartHookCommand is the node one-liner for SessionStart hooks.
// Fires when a Claude Code session begins. Posts session start event to the server.
const sessionStartHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + resolveServerJS + `if(!srvPort)return;const body=JSON.stringify({pid:process.ppid,session_id:i.session_id||''});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/session-start',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

// ReadClaudeSettings reads ~/.claude/settings.json.
// Returns empty map if file does not exist.
func ReadClaudeSettings(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]any{}, nil
		}
		return nil, fmt.Errorf("read claude settings: %w", err)
	}
	var settings map[string]any
	if err := json.Unmarshal(data, &settings); err != nil {
		return nil, fmt.Errorf("parse claude settings: %w", err)
	}
	return settings, nil
}

// HasLazyClaudeHooks checks if the current version of lazyclaude hooks are registered.
// Returns false when hooks exist but are outdated (missing PID liveness check).
func HasLazyClaudeHooks(settings map[string]any) bool {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	// Both hook types must be present AND up-to-date (contain version marker).
	return containsCurrentHook(hooks, "PreToolUse") && containsCurrentHook(hooks, "Notification")
}

// containsCurrentHook checks that the hook type has a lazyclaude hook with the current version marker.
func containsCurrentHook(hooks map[string]any, hookType string) bool {
	entries, ok := hooks[hookType].([]any)
	if !ok {
		return false
	}
	for _, entry := range entries {
		entryMap, ok := entry.(map[string]any)
		if !ok {
			continue
		}
		hookList, ok := entryMap["hooks"].([]any)
		if !ok {
			continue
		}
		for _, h := range hookList {
			hMap, ok := h.(map[string]any)
			if !ok {
				continue
			}
			cmd, _ := hMap["command"].(string)
			if strings.Contains(cmd, hookMarker) && strings.Contains(cmd, hookVersionMarker) {
				return true
			}
		}
	}
	return false
}

// SetLazyClaudeHooks adds or updates lazyclaude hooks in settings.
// Returns a new map (does not mutate input).
func SetLazyClaudeHooks(settings map[string]any) map[string]any {
	result := make(map[string]any, len(settings))
	for k, v := range settings {
		if k != "hooks" {
			result[k] = v
		}
	}

	// Preserve existing hooks, replace lazyclaude entries
	existingHooks, _ := settings["hooks"].(map[string]any)
	newHooks := make(map[string]any)
	if existingHooks != nil {
		for k, v := range existingHooks {
			if k != "PreToolUse" && k != "Notification" {
				newHooks[k] = v
			}
		}
	}

	newHooks["PreToolUse"] = []any{
		map[string]any{
			"matcher": "*",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": preToolUseHookCommand,
				},
			},
		},
	}
	newHooks["Notification"] = []any{
		map[string]any{
			"matcher": "*",
			"hooks": []any{
				map[string]any{
					"type":    "command",
					"command": notificationHookCommand,
				},
			},
		},
	}

	result["hooks"] = newHooks
	return result
}

// BuildHooksSettingsJSON returns a JSON string suitable for the `claude --settings`
// flag. It contains all lazyclaude hooks (PreToolUse, Notification, Stop, SessionStart)
// so they are injected at session startup without modifying ~/.claude/settings.json.
func BuildHooksSettingsJSON() (string, error) {
	settings := map[string]any{
		"hooks": buildHooksMap(),
	}
	data, err := json.Marshal(settings)
	if err != nil {
		return "", fmt.Errorf("marshal hooks settings: %w", err)
	}
	return string(data), nil
}

// buildHooksMap returns the hooks structure with all lazyclaude hook types.
func buildHooksMap() map[string]any {
	hookEntry := func(command string) []any {
		return []any{
			map[string]any{
				"matcher": "*",
				"hooks": []any{
					map[string]any{
						"type":    "command",
						"command": command,
					},
				},
			},
		}
	}
	return map[string]any{
		"PreToolUse":   hookEntry(preToolUseHookCommand),
		"Notification": hookEntry(notificationHookCommand),
		"Stop":         hookEntry(stopHookCommand),
		"SessionStart": hookEntry(sessionStartHookCommand),
	}
}

// WriteClaudeSettings writes settings to a JSON file.
func WriteClaudeSettings(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}
