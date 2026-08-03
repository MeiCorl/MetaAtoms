package security

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/metaatoms/metaatoms/src/tool"
)

func TestCheckBashCommandInSandboxAllowsRelativePathInsideWorkdir(t *testing.T) {
	workdir := t.TempDir()

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = `Get-ChildItem .\workspace\app.txt`
	} else {
		cmd = `ls ./workspace/app.txt`
	}

	if err := CheckBashCommandInSandbox(cmd, workdir); err != nil {
		t.Fatalf("inside relative path should be allowed: %v", err)
	}
}

func TestCheckBashCommandInSandboxRejectsParentTraversal(t *testing.T) {
	workdir := t.TempDir()

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = `Get-Content ..\user_data.dat`
	} else {
		cmd = `cat ../user_data.dat`
	}

	err := CheckBashCommandInSandbox(cmd, workdir)
	if !errors.Is(err, ErrBashPathOutsideSandbox) {
		t.Fatalf("parent traversal should be rejected, got %v", err)
	}
}

func TestCheckBashCommandInSandboxRejectsAbsolutePathOutsideWorkdir(t *testing.T) {
	workdir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "user_data.dat")

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = `Get-Content -Path "` + outside + `"`
	} else {
		cmd = `cat "` + outside + `"`
	}

	err := CheckBashCommandInSandbox(cmd, workdir)
	if !errors.Is(err, ErrBashPathOutsideSandbox) {
		t.Fatalf("outside absolute path should be rejected, got %v", err)
	}
}

func TestCheckBashCommandInSandboxRejectsHomeEnvPath(t *testing.T) {
	workdir := t.TempDir()

	var cmd string
	if runtime.GOOS == "windows" {
		cmd = `Get-Content -Path "$env:USERPROFILE\.metaatoms\user_data.dat"`
	} else {
		cmd = `cat "$HOME/.metaatoms/user_data.dat"`
	}

	err := CheckBashCommandInSandbox(cmd, workdir)
	if !errors.Is(err, ErrBashPathOutsideSandbox) {
		t.Fatalf("home env path should be rejected, got %v", err)
	}
}

func TestCheckerRejectsBashPathOutsideWorkdir(t *testing.T) {
	workdir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "secret.txt")
	raw, err := json.Marshal(map[string]string{
		"command": `cat "` + outside + `"`,
	})
	if err != nil {
		t.Fatalf("marshal input: %v", err)
	}
	var params map[string]interface{}
	if err := json.Unmarshal(raw, &params); err != nil {
		t.Fatalf("unmarshal input: %v", err)
	}

	decision := NewChecker(workdir).Decide(context.Background(), "Bash", params, tool.PermExec)
	if decision.Allowed {
		t.Fatal("checker should reject bash command outside workdir")
	}
	if !strings.Contains(decision.Reason, "bash sandbox denied") {
		t.Fatalf("reason should mention bash sandbox, got %q", decision.Reason)
	}
}

func TestSandboxedProcessEnvRedirectsImplicitUserLocations(t *testing.T) {
	workdir := t.TempDir()
	wantWorkdir := filepath.Clean(workdir)
	if resolved, err := filepath.EvalSymlinks(workdir); err == nil {
		wantWorkdir = filepath.Clean(resolved)
	}
	env := SandboxedProcessEnv([]string{"HOME=/real-home", "USERPROFILE=C:\\real-home", "PATH=/bin"}, workdir)

	if got := envValue(env, "HOME"); got != wantWorkdir {
		t.Fatalf("HOME = %q, want %q", got, wantWorkdir)
	}
	if got := envValue(env, "USERPROFILE"); got != wantWorkdir {
		t.Fatalf("USERPROFILE = %q, want %q", got, wantWorkdir)
	}
	if got := envValue(env, "GIT_CONFIG_NOSYSTEM"); got != "true" {
		t.Fatalf("GIT_CONFIG_NOSYSTEM = %q, want true", got)
	}
}

func envValue(env []string, key string) string {
	needle := key + "="
	if runtime.GOOS == "windows" {
		needle = strings.ToUpper(needle)
	}
	for _, item := range env {
		candidate := item
		if runtime.GOOS == "windows" {
			candidate = strings.ToUpper(candidate)
		}
		if strings.HasPrefix(candidate, needle) {
			return item[len(key)+1:]
		}
	}
	return ""
}
