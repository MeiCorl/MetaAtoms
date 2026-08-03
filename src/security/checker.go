package security

import (
	"context"

	"github.com/metaatoms/metaatoms/src/tool"
)

// Decision is the result of checking one tool invocation before execution.
type Decision struct {
	Allowed    bool
	Reason     string
	TargetPath string
	Workdir    string
}

// Checker enforces MetaAtoms' cloud tenant boundary:
// Bash commands must pass the hard blacklist and path preflight, and file
// read/write tools must stay inside the current user's directory.
type Checker struct {
	workdir string
}

func NewChecker(workdir string) *Checker {
	return &Checker{workdir: workdir}
}

func (c *Checker) CloneIsolated() *Checker {
	if c == nil {
		return NewChecker("")
	}
	return NewChecker(c.workdir)
}

func (c *Checker) CloneIsolatedForWorkdir(workdir string) *Checker {
	return NewChecker(workdir)
}

func (c *Checker) Workdir() string {
	if c == nil {
		return ""
	}
	return c.workdir
}

func (c *Checker) Decide(_ context.Context, toolName string, params map[string]interface{}, _ tool.ToolPermission) Decision {
	if c == nil {
		return Decision{Allowed: false, Reason: "security checker is not configured"}
	}
	if toolName == "Bash" {
		cmd := extractStringParam(params, "command")
		if cmd != "" {
			if err := CheckBashCommandInSandbox(cmd, c.workdir); err != nil {
				return Decision{Allowed: false, Reason: "bash sandbox denied: " + err.Error(), Workdir: c.workdir}
			}
		}
		return Decision{Allowed: true, Reason: "bash allowed after blacklist and path sandbox check", Workdir: c.workdir}
	}
	if paramKey, ok := IsPathTool(toolName); ok {
		pathValue := extractStringParam(params, paramKey)
		if pathValue != "" && c.workdir != "" {
			outside, err := IsPathOutsideSandbox(pathValue, c.workdir)
			if err != nil || outside {
				return Decision{
					Allowed:    false,
					Reason:     "path is outside current user directory",
					TargetPath: pathValue,
					Workdir:    c.workdir,
				}
			}
		}
		return Decision{
			Allowed:    true,
			Reason:     "path is inside current user directory",
			TargetPath: pathValue,
			Workdir:    c.workdir,
		}
	}
	return Decision{Allowed: true, Reason: "non-file tool allowed", Workdir: c.workdir}
}

func extractStringParam(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	v, ok := params[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
