package validatorx

import (
	"errors"
	"regexp"

	"github.com/go-playground/validator/v10"
	"go.uber.org/zap"

	"rapid-admin/internal/pkg/logger"
)

/**
 * RegisterBuiltins 注册框架级自定义规则
 *   - cn_phone:11 位中国手机号(1[3-9]xxxxxxxxx)
 *   - idcard:18 位身份证(末位可 Xx)
 *
 * 业务模块也可以在 init() 里调 RegisterCustom 注册自己的规则
 *
 * ⚠️ **M112(2026-10-29)修正:注册语义按 v10.30.5 实测重写**
 *
 *	老注释写的是"重复注册同名 tag 会 panic("tag already exists")" —— **不对**。
 *	实测(go-playground/validator v10.30.5,见 scripts/probe_m112.mjs):
 *
 *	  - 重复注册同名 tag:**既不 panic 也不报错** —— 直接覆盖注册表里的函数
 *	    ⇒ "靠 panic 兜底"这条防护从来不存在;"重复注册会 panic"那条注释也是错的
 *	  - 注册**受限 tag**(required / dive / omitempty / keys 等内置保留字,
 *	    或名字里含 . [ ] , | = + ( ) ` ~ ! @ # $ % ^ & * \ " / ? < > { }):
 *	    **真的会 panic** —— "Tag 'required' either contains restricted characters
 *	    or is the same as a restricted tag needed for normal operation"
 *	  - 唯一会返回 error 的两种情况:**空 tag 名** / **空函数**
 *
 *	⇒ 旧代码 `_ = v.RegisterCustom(...)` 把错误直接丢掉。对内置这两个 tag 来说
 *	  实际触发不到(名字和函数都不为空),但业务模块调 `RegisterCustom` 时
 *	  一旦传错就是**静默失败**:规则没注册 ⇒ DTO 上的 `validate:"xxx"`
 *	  变成"未知规则" ⇒ 校验时报 "Undefined validation function",
 *	  而且没人知道为什么。
 *
 *	⇒ 现在:注册失败打一条 Error 日志(必须让人看见),
 *	  并且把错误原样返回给调用方(main.go 会打出来)。
 *
 * 业务侧建议加一个 'xxx_' 前缀(例如 `wx_appid`),避免与内置规则撞名。
 */
func RegisterBuiltins() error {
	v := Get()
	var errs []error
	if err := v.RegisterCustom("cn_phone", cnPhone); err != nil {
		errs = append(errs, err)
	}
	if err := v.RegisterCustom("idcard", idCard); err != nil {
		errs = append(errs, err)
	}
	if len(errs) == 0 {
		return nil
	}
	// 立刻打一条 —— logger 在 main 里先于本函数初始化(见 cmd/server/main.go)
	for _, err := range errs {
		logger.L().Error("validatorx: 内置规则注册失败,使用该规则的 DTO 会校验失败",
			zap.Error(err))
	}
	return errors.Join(errs...)
}

var phoneRe = regexp.MustCompile(`^1[3-9]\d{9}$`)

func cnPhone(fl validator.FieldLevel) bool {
	return phoneRe.MatchString(fl.Field().String())
}

var idcardRe = regexp.MustCompile(`^[1-9]\d{5}(18|19|20)\d{2}(0[1-9]|1[0-2])(0[1-9]|[12]\d|3[01])\d{3}[\dXx]$`)

func idCard(fl validator.FieldLevel) bool {
	return idcardRe.MatchString(fl.Field().String())
}
