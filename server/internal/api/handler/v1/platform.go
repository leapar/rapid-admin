// 平台运营 handler(框架占位)
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

type PlatformHandler struct{ *handler.BaseHandler }

func NewPlatformHandler(r *service.Registry) *PlatformHandler {
	return &PlatformHandler{BaseHandler: handler.New(r)}
}

// 占位:平台运营专属操作清单
func (h *PlatformHandler) ListOps(ctx context.Context, c *app.RequestContext) { h.NotImplemented(c, "platform.ops") }
