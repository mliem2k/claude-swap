package cswap

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"time"
)

// authOverrideEnvVars mirrors AUTH_OVERRIDE_ENV_VARS: env vars that make
// claude bypass account OAuth entirely (verified against claude 2.1.175).
// Dropped from the auth-status probe (they'd fake "logged in" for the
// wrong reason) and scrubbed from the session launch env with a warning.
var authOverrideEnvVars = []string{
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_AUTH_TOKEN",
	"CLAUDE_CODE_OAUTH_TOKEN",
	"CLAUDE_CODE_OAUTH_TOKEN_FILE_DESCRIPTOR",
	"CLAUDE_CODE_API_KEY_FILE_DESCRIPTOR",
}

// authStatusTimeout mirrors _AUTH_STATUS_TIMEOUT.
const authStatusTimeout = 10 * time.Second

// probeEnv mirrors _probe_env: env for the auth-status probe (session
// config dir set, auth-override vars dropped). Drops any inherited
// CLAUDE_CONFIG_DIR before appending the session dir's value, mirroring
// Python's dict assignment (a single overwritten key), rather than
// relying on exec.Cmd.Env's own last-value-wins dedup for correctness.
func probeEnv(sessionDir string) []string {
	var env []string
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		if key == "CLAUDE_CONFIG_DIR" {
			continue
		}
		dropped := false
		for _, override := range authOverrideEnvVars {
			if key == override {
				dropped = true
				break
			}
		}
		if !dropped {
			env = append(env, kv)
		}
	}
	env = append(env, "CLAUDE_CONFIG_DIR="+sessionDir)
	return env
}

// authStatusRunner is a test seam: real callers use the default
// implementation (a real `claude auth status --json` subprocess); tests
// substitute a fake so this package's tests never depend on a real
// claude binary being on PATH.
var authStatusRunner = func(claudeBin string, env []string, timeout time.Duration) (stdout string, exitCode int, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, claudeBin, "auth", "status", "--json")
	cmd.Env = env
	var out bytes.Buffer
	cmd.Stdout = &out
	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return "", -1, ctx.Err()
	}
	if exitErr, ok := runErr.(*exec.ExitError); ok {
		return out.String(), exitErr.ExitCode(), nil
	}
	if runErr != nil {
		return "", -1, runErr
	}
	return out.String(), 0, nil
}

// isSessionValid mirrors SessionManager._is_session_valid: whether claude
// sees the profile as logged in with the right identity. Local check only
// (claude auth status makes no API call): a revoked but unexpired token
// still passes and fails on first real use.
func isSessionValid(sessionDir, email, orgUUID string) bool {
	info, err := os.Stat(sessionDir)
	if err != nil || !info.IsDir() {
		return false
	}
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		claudeBin = "claude"
	}
	stdout, exitCode, err := authStatusRunner(claudeBin, probeEnv(sessionDir), authStatusTimeout)
	if err != nil {
		return false
	}
	if exitCode != 0 {
		return false
	}
	var status map[string]any
	if err := json.Unmarshal([]byte(stdout), &status); err != nil {
		return false
	}
	loggedIn, _ := status["loggedIn"].(bool)
	if !loggedIn {
		return false
	}
	// Verified against claude 2.1.175; an env API key reports a different
	// method, and probeEnv already drops those vars anyway.
	authMethod, _ := status["authMethod"].(string)
	if authMethod != "claude.ai" {
		return false
	}
	statusEmail, _ := status["email"].(string)
	if statusEmail != email {
		return false
	}
	// Lenient org check: only when both sides have a value, so schema
	// drift degrades to email-only validation instead of false negatives.
	statusOrg, _ := status["orgId"].(string)
	if statusOrg != "" && orgUUID != "" && statusOrg != orgUUID {
		return false
	}
	return true
}
