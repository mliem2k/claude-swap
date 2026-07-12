package cswap

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// ClaudeSession mirrors ClaudeSession: a running session from
// ~/.claude/sessions/{pid}.json.
type ClaudeSession struct {
	PID        int
	SessionID  string
	Cwd        string
	StartedAt  int64  // epoch milliseconds
	Kind       string // "interactive", "bg", "daemon", "daemon-worker"
	Entrypoint string // "cli", "claude-vscode", "claude-desktop", "sdk-cli", "mcp"
	Status     string // "busy", "idle", "waiting", or ""
}

// IdeInstance mirrors IdeInstance: a running IDE instance from
// ~/.claude/ide/{port}.lock.
type IdeInstance struct {
	Port             int
	PID              int
	IdeName          string
	WorkspaceFolders []string
}

// IsPIDAlive mirrors is_pid_alive: whether a process with the given PID is
// running. Cross-platform: POSIX uses kill(pid, 0); Windows uses OpenProcess.
func IsPIDAlive(pid int) bool {
	if pid <= 1 {
		return false
	}
	return isPIDAlivePlatform(pid)
}

// ListSessions mirrors list_sessions: session PID files with alive processes.
func ListSessions(claudeDir string) []ClaudeSession {
	sessionsDir := filepath.Join(claudeDir, "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return nil
	}
	var sessions []ClaudeSession
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(sessionsDir, e.Name()))
		if err != nil {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		pidF, ok := raw["pid"].(float64)
		if !ok {
			continue
		}
		pid := int(pidF)
		if !IsPIDAlive(pid) {
			continue
		}
		sessions = append(sessions, ClaudeSession{
			PID:        pid,
			SessionID:  stringField(raw, "sessionId"),
			Cwd:        stringField(raw, "cwd"),
			StartedAt:  int64(floatField(raw, "startedAt")),
			Kind:       stringField(raw, "kind"),
			Entrypoint: stringField(raw, "entrypoint"),
			Status:     stringField(raw, "status"),
		})
	}
	return sessions
}

// ListIdeInstances mirrors list_ide_instances: IDE lockfiles with alive
// processes.
func ListIdeInstances(claudeDir string) []IdeInstance {
	ideDir := filepath.Join(claudeDir, "ide")
	entries, err := os.ReadDir(ideDir)
	if err != nil {
		return nil
	}
	var instances []IdeInstance
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".lock") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(ideDir, e.Name()))
		if err != nil {
			continue
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			continue
		}
		pidF, ok := raw["pid"].(float64)
		if !ok {
			continue
		}
		pid := int(pidF)
		if !IsPIDAlive(pid) {
			continue
		}
		port, err := strconv.Atoi(strings.TrimSuffix(e.Name(), ".lock"))
		if err != nil {
			continue
		}
		ideName := stringField(raw, "ideName")
		if ideName == "" {
			ideName = "Unknown IDE"
		}
		var folders []string
		if rawFolders, ok := raw["workspaceFolders"].([]any); ok {
			for _, f := range rawFolders {
				if s, ok := f.(string); ok {
					folders = append(folders, s)
				}
			}
		}
		instances = append(instances, IdeInstance{
			Port: port, PID: pid, IdeName: ideName, WorkspaceFolders: folders,
		})
	}
	return instances
}

// GetRunningInstances mirrors get_running_instances.
func GetRunningInstances(claudeDir string) ([]ClaudeSession, []IdeInstance) {
	return ListSessions(claudeDir), ListIdeInstances(claudeDir)
}

func stringField(m map[string]any, key string) string {
	s, _ := m[key].(string)
	return s
}

func floatField(m map[string]any, key string) float64 {
	f, _ := m[key].(float64)
	return f
}
