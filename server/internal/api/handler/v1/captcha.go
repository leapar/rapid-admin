// 图形验证码 handler
// 文档 §9.2 — base64 PNG,4 位字母+数字,5 分钟过期
package v1

import (
	"context"

	"github.com/cloudwego/hertz/pkg/app"

	"rapid-admin/internal/api/handler"
	"rapid-admin/internal/service"
)

type CaptchaHandler struct{ *handler.BaseHandler }

func NewCaptchaHandler(r *service.Registry) *CaptchaHandler {
	return &CaptchaHandler{BaseHandler: handler.New(r)}
}

// Get GET /rapid-admin/v1/public/captcha
//   响应: { id, image: "data:image/png;base64,...", type, width, height, length, expiresIn }
func (h *CaptchaHandler) Get(ctx context.Context, c *app.RequestContext) {
	res, err := h.Services.Captcha.Generate(ctx)
	if err != nil {
		h.Fail(c, err)
		return
	}
	h.OK(c, res)
}
