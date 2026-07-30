package builtin

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/metaatoms/metaatoms/src/security"
)

func withSandedPath(t *testing.T, sandbox, rel string) context.Context {
	t.Helper()
	return withSandedPathByKey(t, sandbox, rel, "file_path")
}

func withSandedPathByKey(t *testing.T, sandbox, rel, key string) context.Context {
	t.Helper()
	abs, err := security.ResolveInSandbox(filepath.Clean(rel), sandbox)
	if err != nil {
		t.Fatalf("resolve sandbox path: %v", err)
	}
	resolver := security.NewPathResolver()
	resolver.Set(key, abs)
	return security.WithPathResolver(context.Background(), resolver)
}

func errorsIs(err, target error) bool {
	return errors.Is(err, target)
}
