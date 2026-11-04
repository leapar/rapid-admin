// errcode —— 业务错误码
//
// ## 这个包解决什么问题
//
// HTTP 状态码回答"请求成不成功",业务错误码回答"为什么"。
// 两者分开,前端才能既按 HTTP 分支(网关/监控看这个)又按业务码分支(UI 给具体提示)。
//
// ## 关键约定
//
//   - 业务码是**稳定契约**:前端按码分支,文案改了码不能改
//   - 一个业务码映射一个 HTTP 状态码(HTTPStatus)
//   - 错误对象带 Detail —— 校验类错误把字段级明细塞进去,前端能标红到具体字段
//
// ## 跑法
//
//	go run ./examples/errcode
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/errcode"
)

func main() {
	// 一、业务码是稳定契约:按"类别 × 序号"编排,低三位留给具体错误
	fmt.Println("成功          =", errcode.Success)
	fmt.Println("参数缺失      =", errcode.ParamMissing)
	fmt.Println("参数格式错    =", errcode.ParamInvalid)
	fmt.Println("未授权        =", errcode.Unauthorized)
	fmt.Println("令牌过期      =", errcode.TokenExpired)
	fmt.Println("资源不存在    =", errcode.NotFound)
	fmt.Println("内部错误      =", errcode.Internal)

	// 二、HTTP 状态码由业务码推导 —— 不要在 handler 里手写 400/401/403
	fmt.Println()
	fmt.Printf("HTTPStatus(%d 未授权)   = %d\n", errcode.Unauthorized, errcode.HTTPStatus(errcode.Unauthorized))
	fmt.Printf("HTTPStatus(%d 资源不存在) = %d\n", errcode.NotFound, errcode.HTTPStatus(errcode.NotFound))
	fmt.Printf("HTTPStatus(%d 内部错误)   = %d\n", errcode.Internal, errcode.HTTPStatus(errcode.Internal))

	// 三、Detail 带字段级明细 —— 前端据此把错误标红到具体输入框
	fmt.Println()
	fmt.Println("字段级错误:")
	fmt.Println(" ", errcode.Newf(errcode.InvalidParam, "手机号 %s 不是合法号码", "1380000"))

	withDetail := errcode.New(errcode.InvalidParam).WithDetail(map[string]any{
		"field": "phone", "rule": "cn_phone", "value": "1380000",
	})
	fmt.Println(" ", withDetail, "detail =", withDetail.Detail)

	// 四、取码必须用 CodeOf —— 它走 errors.As,能穿透 %w 包装
	//
	// ⚠️ 服务层普遍用 fmt.Errorf("解析第 %d 关审批人失败: %w", n, err) 包错误。
	// 裸类型断言 err.(*Error) 只认最外层,包装一下就失配 ⇒ 一律退化成 500,
	// 真正的业务码在包装层就被吃掉了。
	wrapped := fmt.Errorf("解析第 2 关审批人失败: %w",
		errcode.New(errcode.ApprovalNoAssignee, "审批人解析为空"))
	fmt.Println()
	fmt.Println("包装后取业务码 =", errcode.CodeOf(wrapped), "(期望", errcode.ApprovalNoAssignee, ")")
	fmt.Println("包装后取默认文案 =", errcode.Msg(errcode.CodeOf(wrapped)))
}
