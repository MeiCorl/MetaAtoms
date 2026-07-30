package security

import (
	"context"
	"encoding/json"
	"strings"

	"go.uber.org/zap"

	"github.com/metaatoms/metaatoms/src/logger"
	"github.com/metaatoms/metaatoms/src/tool"
)

var PathTools = map[string]string{
	"ReadFile":  "file_path",
	"WriteFile": "file_path",
	"EditFile":  "file_path",
	"Glob":      "path",
	"Grep":      "path",
}

var PathToolPermissions = map[string]tool.ToolPermission{
	"ReadFile":  tool.PermRead,
	"Glob":      tool.PermRead,
	"Grep":      tool.PermRead,
	"WriteFile": tool.PermWrite,
	"EditFile":  tool.PermWrite,
}

func IsPathTool(toolName string) (paramKey string, ok bool) {
	paramKey, ok = PathTools[toolName]
	return
}

func PathToolPermission(toolName string) (tool.ToolPermission, bool) {
	perm, ok := PathToolPermissions[toolName]
	return perm, ok
}

type PathResolver struct {
	abs map[string]string
}

func NewPathResolver() *PathResolver {
	return &PathResolver{abs: make(map[string]string)}
}

func (r *PathResolver) Set(key, absPath string) {
	r.abs[key] = absPath
}

func (r *PathResolver) Get(key string) (string, bool) {
	v, ok := r.abs[key]
	return v, ok
}

func (r *PathResolver) All() map[string]string {
	out := make(map[string]string, len(r.abs))
	for k, v := range r.abs {
		out[k] = v
	}
	return out
}

type MiddlewareFunc func(
	ctx context.Context,
	toolName string,
	input json.RawMessage,
	perm tool.ToolPermission,
) (outCtx context.Context, err error)

// SandboxMiddleware resolves paths for built-in file tools and rejects any
// target outside the current user's directory. The only non-filesystem exception
// is embedded built-in Skill content, addressed by embedded:// URLs.
func SandboxMiddleware(workdir string) MiddlewareFunc {
	return func(ctx context.Context, toolName string, input json.RawMessage, perm tool.ToolPermission) (context.Context, error) {
		paramKey, isPath := IsPathTool(toolName)
		if !isPath {
			return ctx, nil
		}

		params, err := parseInputParams(input)
		if err != nil {
			logger.WarnCtx(ctx, "SandboxMiddleware failed to parse tool input; using empty params",
				zap.String("tool", toolName),
				zap.Error(err),
			)
			params = nil
		}

		pathStr := extractStringParam(params, paramKey)
		if pathStr == "" {
			if toolName == "Glob" || toolName == "Grep" {
				pathStr = workdir
			} else {
				return ctx, nil
			}
		}

		if perm == tool.PermRead && toolName == "ReadFile" && isEmbeddedBuiltinPath(pathStr) {
			resolver := NewPathResolver()
			resolver.Set(paramKey, strings.TrimSpace(strings.ReplaceAll(pathStr, "\\", "/")))
			return WithPathResolver(ctx, resolver), nil
		}

		absPath, err := ResolveInSandbox(pathStr, workdir)
		if err != nil {
			logger.InfoCtx(ctx, "SandboxMiddleware denied path outside user directory",
				zap.String("tool", toolName),
				zap.String("param_key", paramKey),
				zap.String("raw_path", pathStr),
				zap.String("workdir", workdir),
				zap.Error(err),
			)
			return ctx, err
		}

		resolver := NewPathResolver()
		resolver.Set(paramKey, absPath)
		return WithPathResolver(ctx, resolver), nil
	}
}

type pathResolverKey struct{}

func WithPathResolver(ctx context.Context, resolver *PathResolver) context.Context {
	if resolver == nil {
		return ctx
	}
	return context.WithValue(ctx, pathResolverKey{}, resolver)
}

func PathResolverFromContext(ctx context.Context) (*PathResolver, bool) {
	if ctx == nil {
		return nil, false
	}
	v := ctx.Value(pathResolverKey{})
	if v == nil {
		return nil, false
	}
	r, ok := v.(*PathResolver)
	if !ok || r == nil {
		return nil, false
	}
	return r, true
}

func isEmbeddedBuiltinPath(p string) bool {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	return strings.HasPrefix(p, "embedded://skill/builtin/")
}
