package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

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
// Always reads lock files from ~/.claude/ide/ to find the alive server.
// This ensures hooks always connect to the current server even after restarts.
// Sets `srvPort` (number) and `srvToken` (string), or both null if no server found.
const resolveServerJS = `let srvPort=null,srvToken=null;` +
	findAliveLockJS + `if(best){srvPort=best.port;srvToken=best.lock.authToken;}`

// preToolUseHookCommand is the node one-liner for PreToolUse hooks.
// Resolves server via lock file scanning with PID liveness validation.
const preToolUseHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + resolveServerJS + `if(!srvPort){console.log(d);return;}const body=JSON.stringify({type:'tool_info',pid:process.ppid,tool_name:i.tool_name||'',tool_input:i.tool_input||{}});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/notify',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}console.log(d);})"` //nolint:lll

// notificationHookCommand is the node one-liner for Notification hooks.
// Resolves server via lock file scanning with PID liveness validation.
const notificationHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);if(i.notification_type!=='permission_prompt')return;const http=require('http');` + resolveServerJS + `if(!srvPort)return;const body=JSON.stringify({pid:process.ppid,tool_name:i.tool_name||'',tool_input:i.tool_input||{},message:i.message||''});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/notify',method:'POST',timeout:2000,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

// stopHookCommand is the node one-liner for Stop hooks.
const stopHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + resolveServerJS + `if(!srvPort)return;const body=JSON.stringify({pid:process.ppid,stop_reason:i.stop_reason||'',session_id:i.session_id||''});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/stop',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

// sessionStartHookCommand is the node one-liner for SessionStart hooks.
const sessionStartHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + resolveServerJS + `if(!srvPort)return;const body=JSON.stringify({pid:process.ppid,session_id:i.session_id||''});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/session-start',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

// userPromptSubmitHookCommand is the node one-liner for UserPromptSubmit hooks.
const userPromptSubmitHookCommand = `node -e "let d='';process.stdin.on('data',c=>d+=c);process.stdin.on('end',()=>{try{const i=JSON.parse(d);const http=require('http');` + resolveServerJS + `if(!srvPort)return;const body=JSON.stringify({pid:process.ppid,session_id:i.session_id||''});const req=http.request({hostname:'127.0.0.1',port:srvPort,path:'/prompt-submit',method:'POST',timeout:300,headers:{'Content-Type':'application/json','Content-Length':Buffer.byteLength(body),'X-Claude-Code-Ide-Authorization':srvToken}});req.on('error',()=>{});req.on('timeout',()=>{req.destroy()});req.write(body);req.end();}catch{}})"` //nolint:lll

// WriteHooksSettingsFile writes the hooks settings JSON to a file and returns the path.
// The file is placed in the lazyclaude runtime directory so it persists for the
// process lifetime. Using a file avoids shell quoting issues with --settings flag.
func WriteHooksSettingsFile(runtimeDir string) (string, error) {
	settings := map[string]any{
		"hooks": buildHooksMap(),
	}

	// Use json.Encoder with SetEscapeHTML(false) to prevent Go's default
	// HTML-safe escaping of >, <, & to \u003e, \u003c, \u0026.
	// Hook commands contain => (arrow functions) which must remain literal
	// for node to parse them correctly.
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(settings); err != nil {
		return "", fmt.Errorf("marshal hooks settings: %w", err)
	}

	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return "", fmt.Errorf("create runtime dir: %w", err)
	}

	path := runtimeDir + "/hooks-settings.json"
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		return "", fmt.Errorf("write hooks settings file: %w", err)
	}
	return path, nil
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
		"PreToolUse":        hookEntry(preToolUseHookCommand),
		"Notification":      hookEntry(notificationHookCommand),
		"Stop":              hookEntry(stopHookCommand),
		"SessionStart":      hookEntry(sessionStartHookCommand),
		"UserPromptSubmit":  hookEntry(userPromptSubmitHookCommand),
	}
}

