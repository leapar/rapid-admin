// validatorx —— 统一参数校验
//
// ## 这个包解决什么问题
//
// DTO 上的规则用 `validate:"..."` 标签声明,校验逻辑集中在一处,
// 错误信息统一是中文(跟业务码的 defaultMsg 风格一致)。
//
// ## 关键点
//
//   - 单例 `validatorx.Get()`,线程安全,全局复用
//   - 字段名取 **json tag**,不是 Go 字段名 ⇒ 前端对得上
//   - `Explain` 用 errors.As 拆错误 ⇒ 能穿透任意层 %w 包装
//     (裸类型断言只认最外层,包装一下字段名就整个丢掉)
//   - 第一个失败字段即返回,不一次抛一堆吓用户
//   - 框架级自定义规则:cn_phone(11 位手机号)、idcard(18 位身份证)
//
// ## 跑法
//
//	go run ./examples/validatorx
package main

import (
	"fmt"

	"github.com/go-playground/validator/v10"

	"rapid-admin/internal/pkg/validatorx"
)

// loginDTO 登录请求 —— 规则全在标签上
type loginDTO struct {
	Phone    string `json:"phone" validate:"required,cn_phone"` // 手机号必填且格式要对
	Password string `json:"password" validate:"required,min=10,max=32"`
	Code     string `json:"code" validate:"omitempty,len=6"` // 图形验证码(可选,但给了就必须是 6 位)
	Email    string `json:"email" validate:"omitempty,email"`
}

func main() {
	// 一、注册框架级内置规则(在 main 装配阶段调一次即可)
	if err := validatorx.RegisterBuiltins(); err != nil {
		fmt.Println("注册内置规则失败:", err)
		return
	}

	v := validatorx.Get()

	// 二、校验:失败时拿"字段名 + 中文原因"
	show := func(label string, dto any) {
		err := v.Struct(dto)
		if err == nil {
			fmt.Printf("%-12s 通过\n", label)
			return
		}
		field, msg := v.Explain(err)
		fmt.Printf("%-12s 失败 → 字段 %q: %s\n", label, field, msg)
	}

	fmt.Println("校验结果:")
	show("全部合法", loginDTO{Phone: "13800001234", Password: "Abcd1234!@", Code: "123456"})
	show("手机号缺失", loginDTO{Password: "Abcd1234!@"})
	show("手机号格式错", loginDTO{Phone: "1380000", Password: "Abcd1234!@"})
	show("密码太短", loginDTO{Phone: "13800001234", Password: "short"})
	show("验证码位数错", loginDTO{Phone: "13800001234", Password: "Abcd1234!@", Code: "12"})
	show("邮箱格式错", loginDTO{Phone: "13800001234", Password: "Abcd1234!@", Email: "not-an-email"})

	// 三、注册业务自己的规则
	//
	// ⚠️ 建议加业务前缀(如 "wx_appid"),避免跟内置保留字撞名 ——
	// 注册受限 tag(required / dive / omitempty 等)会**直接 panic**。
	if err := v.RegisterCustom("starts_with_hz", func(fl validator.FieldLevel) bool {
		return len(fl.Field().String()) >= 2 && fl.Field().String()[:2] == "hz"
	}); err != nil {
		fmt.Println("注册自定义规则失败:", err)
		return
	}

	type bizDTO struct {
		Code string `json:"code" validate:"starts_with_hz"`
	}
	show("自定义规则", bizDTO{Code: "hz001"})
	show("自定义规则(不匹配)", bizDTO{Code: "sh001"})
}
