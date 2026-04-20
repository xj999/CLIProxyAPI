package helps

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	cliproxyauth "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/auth"
	cliproxyexecutor "github.com/router-for-me/CLIProxyAPI/v6/sdk/cliproxy/executor"
)

func ResolveUsageSessionIndex(ctx context.Context, payload []byte, opts cliproxyexecutor.Options) string {
	if len(opts.Metadata) > 0 {
		if raw, ok := opts.Metadata[cliproxyexecutor.ExecutionSessionMetadataKey]; ok && raw != nil {
			switch value := raw.(type) {
			case string:
				if trimmed := strings.TrimSpace(value); trimmed != "" {
					return trimmed
				}
			case []byte:
				if trimmed := strings.TrimSpace(string(value)); trimmed != "" {
					return trimmed
				}
			}
		}
	}

	return cliproxyauth.ExtractSessionID(headersFromContext(ctx), payload, opts.Metadata)
}

func headersFromContext(ctx context.Context) http.Header {
	if ctx == nil {
		return nil
	}
	ginCtx, ok := ctx.Value("gin").(*gin.Context)
	if !ok || ginCtx == nil || ginCtx.Request == nil {
		return nil
	}
	return ginCtx.Request.Header
}
