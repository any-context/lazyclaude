package config

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Hook marker: commands containing this path are identified as lazyclaude hooks.
const hookMarker = "/notify"

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

// preToolUseHookCommand is the node one-liner for PreToolUse hooks.
// Validates lock PID liveness and picks the most recent server.
const preToolUseHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + findAliveLockJS + `if(!best){console.log(d);return;}const body=JSON.stringify({type:'tool_info',pid:process.ppid,tool_name:i.tool_name||'',tool_input:i.tool_input||{}});const req=http.request({hostname:'127.0.0.1',port:best.port,path:'/notify',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':best.lock.authToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}console.log(d);})"` //nolint:lll

// notificationHookCommand is the node one-liner for Notification hooks.
// Validates lock PID liveness and picks the most recent server.
const notificationHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);if(i.notification_type!=='permission_prompt')return;const http=require('http');` + findAliveLockJS + `if(!best)return;const body=JSON.stringify({pid:process.ppid,tool_name:i.tool_name||'',tool_input:i.tool_input||{},message:i.message||''});const req=http.request({hostname:'127.0.0.1',port:best.port,path:'/notify',method:'POST',timeout:2000,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':best.lock.authToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

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

// HasLazyClaudeHooks checks if lazyclaude hooks are already registered.
func HasLazyClaudeHooks(settings map[string]any) bool {
	hooks, ok := settings["hooks"].(map[string]any)
	if !ok {
		return false
	}
	return containsHookMarker(hooks, "PreToolUse") || containsHookMarker(hooks, "Notification")
}

func containsHookMarker(hooks map[string]any, hookType string) bool {
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
			if strings.Contains(cmd, hookMarker) {
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

// WriteClaudeSettings writes settings to a JSON file.
func WriteClaudeSettings(path string, settings map[string]any) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}
