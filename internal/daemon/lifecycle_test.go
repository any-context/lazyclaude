package daemon

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestStartRemoteDaemon_Success(t *testing.T) {
	ssh := newMockSSH()
	cmd := "nohup lazyclaude daemon --port 0 > /tmp/lazyclaude-daemon.log 2>&1 & for i in $(seq 1 20); do sleep 0.5 && [ -f /tmp/lazyclaude-$(whoami)/daemon.json ] && cat /tmp/lazyclaude-$(whoami)/daemon.json && exit 0; done; exit 1"
	ssh.onRun(cmd, `{"port":12345,"token":"abc123"}`, nil)

	lm := NewLifecycleManager(ssh)
	info, err := lm.StartRemoteDaemon(context.Background(), "user@host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Host != "user@host" {
		t.Errorf("Host = %q, want %q", info.Host, "user@host")
	}
	if info.Port != 12345 {
		t.Errorf("Port = %d, want %d", info.Port, 12345)
	}
	if info.Token != "abc123" {
		t.Errorf("Token = %q, want %q", info.Token, "abc123")
	}
}

func TestStartRemoteDaemon_SSHError(t *testing.T) {
	ssh := newMockSSH()
	cmd := "nohup lazyclaude daemon --port 0 > /tmp/lazyclaude-daemon.log 2>&1 & for i in $(seq 1 20); do sleep 0.5 && [ -f /tmp/lazyclaude-$(whoami)/daemon.json ] && cat /tmp/lazyclaude-$(whoami)/daemon.json && exit 0; done; exit 1"
	ssh.onRun(cmd, "", fmt.Errorf("connection refused"))

	lm := NewLifecycleManager(ssh)
	_, err := lm.StartRemoteDaemon(context.Background(), "user@host")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "lazyclaude is not installed on") {
		t.Errorf("error = %q, want to contain 'lazyclaude is not installed on'", err)
	}
}

func TestStartRemoteDaemon_InvalidOutput(t *testing.T) {
	ssh := newMockSSH()
	cmd := "nohup lazyclaude daemon --port 0 > /tmp/lazyclaude-daemon.log 2>&1 & for i in $(seq 1 20); do sleep 0.5 && [ -f /tmp/lazyclaude-$(whoami)/daemon.json ] && cat /tmp/lazyclaude-$(whoami)/daemon.json && exit 0; done; exit 1"
	ssh.onRun(cmd, "not json at all", nil)

	lm := NewLifecycleManager(ssh)
	_, err := lm.StartRemoteDaemon(context.Background(), "user@host")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to parse daemon info") {
		t.Errorf("error = %q, want to contain 'failed to parse daemon info'", err)
	}
}

func TestStopRemoteDaemon_Success(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("lazyclaude daemon stop", "", nil)

	lm := NewLifecycleManager(ssh)
	if err := lm.StopRemoteDaemon(context.Background(), "user@host"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestStopRemoteDaemon_Error(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("lazyclaude daemon stop", "", fmt.Errorf("no daemon running"))

	lm := NewLifecycleManager(ssh)
	err := lm.StopRemoteDaemon(context.Background(), "user@host")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "failed to stop daemon") {
		t.Errorf("error = %q, want to contain 'failed to stop daemon'", err)
	}
}

func TestDiscoverRemoteDaemon_Success(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("cat /tmp/lazyclaude-$(whoami)/daemon.json",
		`{"port":5555,"token":"secret"}`, nil)

	lm := NewLifecycleManager(ssh)
	info, err := lm.DiscoverRemoteDaemon(context.Background(), "user@host")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Host != "user@host" {
		t.Errorf("Host = %q, want %q", info.Host, "user@host")
	}
	if info.Port != 5555 {
		t.Errorf("Port = %d, want %d", info.Port, 5555)
	}
	if info.Token != "secret" {
		t.Errorf("Token = %q, want %q", info.Token, "secret")
	}
}

func TestDiscoverRemoteDaemon_NoFile(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("cat /tmp/lazyclaude-$(whoami)/daemon.json",
		"", fmt.Errorf("No such file or directory"))

	lm := NewLifecycleManager(ssh)
	_, err := lm.DiscoverRemoteDaemon(context.Background(), "user@host")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "no daemon found") {
		t.Errorf("error = %q, want to contain 'no daemon found'", err)
	}
}

func TestDiscoverRemoteDaemon_InvalidJSON(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("cat /tmp/lazyclaude-$(whoami)/daemon.json", "not json", nil)

	lm := NewLifecycleManager(ssh)
	_, err := lm.DiscoverRemoteDaemon(context.Background(), "user@host")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "invalid daemon info") {
		t.Errorf("error = %q, want to contain 'invalid daemon info'", err)
	}
}

func TestDiscoverRemoteDaemon_ZeroPort(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("cat /tmp/lazyclaude-$(whoami)/daemon.json",
		`{"port":0,"token":"abc"}`, nil)

	lm := NewLifecycleManager(ssh)
	_, err := lm.DiscoverRemoteDaemon(context.Background(), "user@host")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "port is 0") {
		t.Errorf("error = %q, want to contain 'port is 0'", err)
	}
}

func TestDiscoverRemoteDaemon_EmptyToken(t *testing.T) {
	ssh := newMockSSH()
	ssh.onRun("cat /tmp/lazyclaude-$(whoami)/daemon.json",
		`{"port":5555,"token":""}`, nil)

	lm := NewLifecycleManager(ssh)
	_, err := lm.DiscoverRemoteDaemon(context.Background(), "user@host")
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "empty token") {
		t.Errorf("error = %q, want to contain 'empty token'", err)
	}
}

func TestParseDaemonOutput(t *testing.T) {
	tests := []struct {
		name    string
		output  string
		want    *DaemonInfo
		wantErr bool
	}{
		{
			name:   "clean JSON",
			output: `{"port":1234,"token":"tok"}`,
			want:   &DaemonInfo{Port: 1234, Token: "tok"},
		},
		{
			name:   "JSON with noise before",
			output: "starting daemon...\n{\"port\":5678,\"token\":\"t\"}\n",
			want:   &DaemonInfo{Port: 5678, Token: "t"},
		},
		{
			name:    "empty",
			output:  "",
			wantErr: true,
		},
		{
			name:    "no JSON",
			output:  "some log output\nanother line\n",
			wantErr: true,
		},
		{
			name:    "port 0",
			output:  `{"port":0,"token":"abc"}`,
			wantErr: true,
		},
		{
			name:    "empty token",
			output:  `{"port":1234,"token":""}`,
			wantErr: true,
		},
		{
			name:    "malformed JSON with brace prefix",
			output:  "{bad json}\n",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDaemonOutput(tt.output)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseDaemonOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if tt.want != nil {
				if got.Port != tt.want.Port {
					t.Errorf("Port = %d, want %d", got.Port, tt.want.Port)
				}
				if got.Token != tt.want.Token {
					t.Errorf("Token = %q, want %q", got.Token, tt.want.Token)
				}
			}
		})
	}
}
