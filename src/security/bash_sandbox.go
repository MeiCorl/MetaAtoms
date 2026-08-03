package security

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
)

// ErrBashPathOutsideSandbox is returned when a shell command explicitly
// references a filesystem path outside the active user's workdir.
var ErrBashPathOutsideSandbox = errors.New("Bash 沙箱拦截：命令引用了当前用户目录之外的路径")

// BashPathOutsideSandboxError carries the offending path for audit logs.
type BashPathOutsideSandboxError struct {
	Command      string
	Path         string
	ResolvedPath string
	Workdir      string
}

func (e *BashPathOutsideSandboxError) Error() string {
	if e == nil {
		return ErrBashPathOutsideSandbox.Error()
	}
	if e.ResolvedPath != "" && e.ResolvedPath != e.Path {
		return fmt.Sprintf("%s: %q -> %q 不在 %q 之内", ErrBashPathOutsideSandbox, e.Path, e.ResolvedPath, e.Workdir)
	}
	return fmt.Sprintf("%s: %q 不在 %q 之内", ErrBashPathOutsideSandbox, e.Path, e.Workdir)
}

func (e *BashPathOutsideSandboxError) Unwrap() error {
	return ErrBashPathOutsideSandbox
}

var (
	psEnvBracedPattern   = regexp.MustCompile(`(?i)\$\{env:([A-Za-z_][A-Za-z0-9_]*)\}`)
	psEnvPattern         = regexp.MustCompile(`(?i)\$env:([A-Za-z_][A-Za-z0-9_]*)`)
	posixEnvBracePattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	posixEnvPattern      = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
	percentEnvPattern    = regexp.MustCompile(`%([A-Za-z_][A-Za-z0-9_]*)%`)
)

// CheckBashCommandInSandbox applies the destructive-command blacklist and then
// rejects shell commands with explicit paths outside workdir. This is a
// defense-in-depth preflight for Bash; OS/container-level isolation is still
// required for complete protection against dynamically constructed paths.
func CheckBashCommandInSandbox(command, workdir string) error {
	if err := CheckBashCommand(command); err != nil {
		return err
	}

	absWorkdir, err := resolveBashSandboxWorkdir(workdir)
	if err != nil {
		return err
	}

	for _, ref := range extractBashPathRefs(command) {
		expanded, unresolved := expandBashPathRef(ref, absWorkdir)
		if unresolved {
			return &BashPathOutsideSandboxError{
				Command:      command,
				Path:         ref,
				ResolvedPath: "unresolved shell variable",
				Workdir:      absWorkdir,
			}
		}
		if expanded == "" {
			continue
		}
		outside, err := IsPathOutsideSandbox(expanded, absWorkdir)
		if err != nil || outside {
			return &BashPathOutsideSandboxError{
				Command:      command,
				Path:         ref,
				ResolvedPath: expanded,
				Workdir:      absWorkdir,
			}
		}
	}

	return nil
}

// SandboxedProcessEnv returns an environment for command subprocesses where
// common implicit file locations resolve inside workdir. This prevents tools
// such as git, npm, go, and shells from reading host/global user config via
// inherited HOME/USERPROFILE/TEMP-style variables.
func SandboxedProcessEnv(base []string, workdir string) []string {
	absWorkdir, err := resolveBashSandboxWorkdir(workdir)
	if err != nil {
		absWorkdir = filepath.Clean(workdir)
	}

	env := make(map[string]string, len(base)+16)
	var order []string
	set := func(key, value string) {
		if key == "" {
			return
		}
		normalized := envKey(key)
		if _, exists := env[normalized]; !exists {
			order = append(order, key)
		}
		env[normalized] = key + "=" + value
	}

	for _, item := range base {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			continue
		}
		set(key, value)
	}

	set("HOME", absWorkdir)
	set("USERPROFILE", absWorkdir)
	set("APPDATA", absWorkdir)
	set("LOCALAPPDATA", absWorkdir)
	set("TEMP", absWorkdir)
	set("TMP", absWorkdir)
	set("PWD", absWorkdir)
	set("XDG_CONFIG_HOME", filepath.Join(absWorkdir, ".config"))
	set("XDG_CACHE_HOME", filepath.Join(absWorkdir, ".cache"))
	set("GIT_CONFIG_GLOBAL", filepath.Join(absWorkdir, ".gitconfig"))
	set("GIT_CONFIG_NOSYSTEM", "true")
	set("NPM_CONFIG_USERCONFIG", filepath.Join(absWorkdir, ".npmrc"))
	set("NPM_CONFIG_CACHE", filepath.Join(absWorkdir, ".npm-cache"))
	set("GOPATH", filepath.Join(absWorkdir, ".go"))
	set("GOMODCACHE", filepath.Join(absWorkdir, ".go", "pkg", "mod"))
	set("GOCACHE", filepath.Join(absWorkdir, ".cache", "go-build"))

	out := make([]string, 0, len(env))
	for _, key := range order {
		if item, ok := env[envKey(key)]; ok {
			out = append(out, item)
		}
	}
	return out
}

func envKey(key string) string {
	if runtime.GOOS == "windows" {
		return strings.ToUpper(key)
	}
	return key
}

func resolveBashSandboxWorkdir(workdir string) (string, error) {
	wd := strings.TrimSpace(workdir)
	if wd == "" {
		var err error
		wd, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("Bash 沙箱无法确定工作目录: %w", err)
		}
	}
	abs, err := filepath.Abs(wd)
	if err != nil {
		return "", fmt.Errorf("Bash 沙箱解析工作目录失败: %w", err)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return filepath.Clean(abs), nil
}

func extractBashPathRefs(command string) []string {
	seen := make(map[string]struct{})
	var refs []string
	for _, word := range splitShellWords(command) {
		for _, candidate := range pathCandidatesFromWord(word) {
			if _, ok := seen[candidate]; ok {
				continue
			}
			seen[candidate] = struct{}{}
			refs = append(refs, candidate)
		}
	}
	return refs
}

func splitShellWords(s string) []string {
	var words []string
	var b strings.Builder
	var quote rune

	flush := func() {
		if b.Len() == 0 {
			return
		}
		words = append(words, b.String())
		b.Reset()
	}

	for _, r := range s {
		if quote != 0 {
			if r == quote {
				quote = 0
				continue
			}
			b.WriteRune(r)
			continue
		}

		switch {
		case r == '\'' || r == '"' || r == '`':
			quote = r
		case unicode.IsSpace(r) || strings.ContainsRune("|&;<>()[]", r):
			flush()
		default:
			b.WriteRune(r)
		}
	}
	flush()
	return words
}

func pathCandidatesFromWord(word string) []string {
	word = cleanShellPathToken(word)
	if word == "" {
		return nil
	}

	parts := []string{word}
	if idx := strings.Index(word, "="); idx >= 0 && idx+1 < len(word) {
		parts = append(parts, word[idx+1:])
	}
	if idx := strings.LastIndex(word, ":"); idx >= 0 && idx+1 < len(word) && !looksLikeWindowsDrivePath(word) {
		parts = append(parts, word[idx+1:])
	}

	var out []string
	seen := make(map[string]struct{})
	for _, part := range parts {
		part = cleanShellPathToken(part)
		if part == "" || !looksLikeShellPath(part) {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func cleanShellPathToken(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"'`)
	s = strings.TrimLeftFunc(s, func(r rune) bool {
		return strings.ContainsRune("([", r)
	})
	s = strings.TrimRightFunc(s, func(r rune) bool {
		return strings.ContainsRune(",;)]", r)
	})
	return strings.TrimSpace(s)
}

func looksLikeShellPath(s string) bool {
	if s == "" {
		return false
	}
	lower := strings.ToLower(s)
	if strings.Contains(lower, "://") && !strings.HasPrefix(lower, "file://") {
		return false
	}
	if hasPathEnvRef(s) || strings.HasPrefix(s, "~") {
		return true
	}
	if strings.HasPrefix(s, "/") || strings.HasPrefix(s, `\`) {
		return true
	}
	if looksLikeWindowsDrivePath(s) || strings.HasPrefix(s, `\\`) {
		return true
	}
	if hasParentSegment(s) || s == "." || strings.HasPrefix(s, "./") || strings.HasPrefix(s, `.\`) {
		return true
	}
	return strings.ContainsAny(s, `/\`)
}

func hasPathEnvRef(s string) bool {
	lower := strings.ToLower(s)
	for _, marker := range []string{
		"$env:home", "$env:userprofile", "${env:home}", "${env:userprofile}",
		"$home", "${home}", "%home%", "%userprofile%",
		"$env:appdata", "$env:localappdata", "%appdata%", "%localappdata%",
		"$env:temp", "$env:tmp", "$tmp", "$temp", "%temp%", "%tmp%",
		"$env:programdata", "%programdata%", "$env:systemroot", "$env:windir", "%systemroot%", "%windir%",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func hasParentSegment(s string) bool {
	normalized := strings.ReplaceAll(s, `\`, "/")
	if normalized == ".." {
		return true
	}
	return strings.HasPrefix(normalized, "../") ||
		strings.HasSuffix(normalized, "/..") ||
		strings.Contains(normalized, "/../")
}

func looksLikeWindowsDrivePath(s string) bool {
	return len(s) >= 3 &&
		((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) &&
		s[1] == ':' &&
		(s[2] == '\\' || s[2] == '/')
}

func expandBashPathRef(ref, workdir string) (string, bool) {
	p := cleanShellPathToken(ref)
	p = strings.TrimPrefix(p, "file://")

	var unresolved bool
	p, unresolved = expandShellEnvPatterns(p, workdir)
	if unresolved {
		return p, true
	}

	if strings.HasPrefix(p, "~") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return p, true
		}
		if p == "~" {
			p = home
		} else if len(p) > 1 && isAnySeparator(p[1]) {
			p = filepath.Join(home, p[2:])
		} else {
			return p, true
		}
	}

	if runtime.GOOS == "windows" && (strings.HasPrefix(p, "/") || (strings.HasPrefix(p, `\`) && !strings.HasPrefix(p, `\\`))) {
		if volume := filepath.VolumeName(workdir); volume != "" {
			p = volume + filepath.FromSlash(p)
		}
	}

	if runtime.GOOS == "windows" {
		p = filepath.FromSlash(p)
	}

	return p, false
}

func expandShellEnvPatterns(s, workdir string) (string, bool) {
	var unresolved bool
	replacer := func(name string) string {
		value, ok := shellEnvValue(name, workdir)
		if !ok {
			unresolved = true
			return name
		}
		return value
	}

	s = replaceEnvPattern(s, psEnvBracedPattern, replacer)
	s = replaceEnvPattern(s, psEnvPattern, replacer)
	s = replaceEnvPattern(s, percentEnvPattern, replacer)
	s = replaceEnvPattern(s, posixEnvBracePattern, replacer)
	s = replaceEnvPattern(s, posixEnvPattern, replacer)
	return s, unresolved
}

func replaceEnvPattern(s string, re *regexp.Regexp, valueFor func(string) string) string {
	return re.ReplaceAllStringFunc(s, func(match string) string {
		sub := re.FindStringSubmatch(match)
		if len(sub) < 2 {
			return match
		}
		return valueFor(sub[1])
	})
}

func shellEnvValue(name, workdir string) (string, bool) {
	upper := strings.ToUpper(name)
	if upper == "PWD" || upper == "CD" {
		return workdir, true
	}
	if value := os.Getenv(upper); value != "" {
		return value, true
	}
	if upper == "HOME" || upper == "USERPROFILE" {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return home, true
		}
	}
	return "", false
}

func isAnySeparator(b byte) bool {
	return b == '/' || b == '\\'
}
