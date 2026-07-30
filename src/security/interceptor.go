package security

import (
	"context"
	"encoding/json"

	"go.uber.org/zap"

	"github.com/metaatoms/metaatoms/src/logger"
	"github.com/metaatoms/metaatoms/src/tool"
)

type InterceptorResult struct {
	Decision Decision
}

type Interceptor struct {
	checker *Checker
}

func NewInterceptor(checker *Checker) *Interceptor {
	return &Interceptor{checker: checker}
}

func (i *Interceptor) Check(ctx context.Context, toolName string, input json.RawMessage, perm tool.ToolPermission) (*InterceptorResult, error) {
	if i == nil || i.checker == nil {
		return &InterceptorResult{Decision: Decision{Allowed: false, Reason: "security interceptor is not configured"}}, nil
	}
	params, err := parseInputParams(input)
	if err != nil {
		logger.WarnCtx(ctx, "security interceptor failed to parse tool input; using empty params",
			zap.String("tool", toolName),
			zap.Error(err),
		)
		params = nil
	}

	decision := i.checker.Decide(ctx, toolName, params, perm)
	if decision.Allowed {
		logger.DebugCtx(ctx, "security check allowed",
			zap.String("tool", toolName),
			zap.String("reason", decision.Reason),
		)
		return nil, nil
	}

	logger.InfoCtx(ctx, "security check denied",
		zap.String("tool", toolName),
		zap.String("reason", decision.Reason),
	)
	return &InterceptorResult{Decision: decision}, nil
}

func parseInputParams(input json.RawMessage) (map[string]interface{}, error) {
	if len(input) == 0 {
		return nil, nil
	}
	var params map[string]interface{}
	if err := json.Unmarshal(input, &params); err != nil {
		return nil, err
	}
	return params, nil
}
