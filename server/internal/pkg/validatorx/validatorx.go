// Package validatorx —— 统一参数校验(§1.11 #6 P1)
//
// 设计:
//   - 单例 *validator.Validate(线程安全,业务全局复用)
//   - 复用 go-playground/validator/v10 + 中文翻译(返回中文错误信息,跟业务码 defaultMsg 风格一致)
//   - DTO 用 `validate:"required,email,min=6,max=32,oneof=foo bar"` 标签声明规则
//   - 必填字符串字段 `binding:"required"` 也兼容(hertz 自身有 binding tag,这里补一刀)
//
// 错误返回:
//   - 第一个失败字段即返回(避免一次抛一堆吓到用户)
//   - 错误码统一 errcode.InvalidParam(40100),Detail 字段给 { field, rule, value, message }
//   - 配合 BaseHandler.BindAndValidate() 一行调
//
// 自定义规则注册在 RegisterCustom(),init 阶段调用一次即可
package validatorx

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"

	"github.com/go-playground/validator/v10"
)

// 全局单例
var (
	once     sync.Once
	instance *Validator
)

// Validator 封装 validator + 中文翻译
type Validator struct {
	// mu 保护 v 内部 validations map 的读改写。
	//
	// ⚠️ v10.30.5 的这张 map **读写都没锁**(cache.go:289 在 Struct 路径上读、
	// validator_instance.go:756 在 RegisterValidation 时裸写),并发注册 +
	// 并发 Struct() 会撞 "concurrent map read and map write" fatal 整个进程。
	// 注册走写锁、校验走读锁:校验高频、注册只在 init/运行期偶发,读锁开销
	// 可忽略;代价是注册期间校验会短暂串行 —— 可接受。
	mu sync.RWMutex
	v  *validator.Validate
}

// Get 取单例
func Get() *Validator {
	once.Do(func() {
		v := validator.New(validator.WithRequiredStructEnabled())
		// 用 JSON tag 名作为字段名(否则返 Go 字段名 UserName,前端对不上)
		v.RegisterTagNameFunc(func(fld reflect.StructField) string {
			tag := fld.Tag.Get("json")
			if tag == "" {
				return fld.Name
			}
			// 去掉 ",omitempty" 等 options
			if i := strings.IndexByte(tag, ','); i >= 0 {
				tag = tag[:i]
			}
			if tag == "-" {
				return fld.Name
			}
			return tag
		})
		instance = &Validator{v: v}
	})
	return instance
}

// Struct 校验 DTO,失败返 (field, message) + 原始 error
//
// ⚠️ 读锁不是可有可无:Struct 路径会读 v 内部的 validations map(为类型构建
// 字段缓存时),与 RegisterCustom 的裸 map 写并发即 fatal(见 mu 字段注释)。
func (val *Validator) Struct(dto any) error {
	val.mu.RLock()
	defer val.mu.RUnlock()
	return val.v.Struct(dto)
}

// RegisterCustom 注册自定义校验规则(全局)
//
// ⚠️ 运行期注册也安全:写锁与 Struct 的读锁互斥,注册瞬间不会有校验并发读
// validations map。init 阶段注册自然没问题,业务模块在运行期按需注册同样行。
func (val *Validator) RegisterCustom(tag string, fn validator.Func) error {
	val.mu.Lock()
	defer val.mu.Unlock()
	return val.v.RegisterValidation(tag, fn, true)
}

/**
 * FieldError 包装成可读中文
 *   - 优先用 tag 翻译表(覆盖 80% 常用规则)
 *   - 没命中时 fallback 到原 message
 *   - 失败时拼 "field: 中文原因" 形式
 *
 * ⚠️ **M112(2026-10-29)修正:原来用的是类型断言,不是 errors.As**
 *
 *	`err.(validator.ValidationErrors)` 只在 err **正好**是那个类型时成立。
 *	任何一层 `fmt.Errorf("...: %w", err)` 的包装都会让它失配
 *	⇒ 字段名整个丢掉,用户只看到"参数校验失败",不知道是哪个字段错了。
 *	⇒ 改用 errors.As(能穿透任意层 %w 包装)。
 */
func (val *Validator) Explain(err error) (field, message string) {
	var ves validator.ValidationErrors
	if !errors.As(err, &ves) || len(ves) == 0 {
		return "", "参数校验失败"
	}
	ve := ves[0]
	field = ve.Field() // 已经被 RegisterTagNameFunc 替换成 json tag
	rule := ve.Tag()
	message = ruleLabel(rule, ve.Param())
	return field, message
}

// ruleLabels 无参数规则 → 中文短句
var ruleLabels = map[string]string{
	"required": "不能为空",
	"email":    "邮箱格式不正确",
	"url":      "URL 格式不正确",
	"uuid":     "UUID 格式不正确",
	"numeric":  "必须是数字",
	"alpha":    "只能是字母",
	"alphanum": "只能是字母和数字",
	"cn_phone": "手机号格式不正确(11 位,1 开头)",
	"phone":    "手机号格式不正确(11 位,1 开头)",
	"idcard":   "身份证号格式不正确",
}

// ruleLabelsWithParam 带参数规则 → 中文短句模板(%s 处填参数)
var ruleLabelsWithParam = map[string]string{
	"min":   "长度不能小于 %s",
	"max":   "长度不能大于 %s",
	"len":   "长度必须为 %s",
	"gte":   "不能小于 %s",
	"gt":    "必须大于 %s",
	"lte":   "不能大于 %s",
	"lt":    "必须小于 %s",
	"oneof": "必须是 %s 之一",
}

// ruleLabel 规则 → 中文短句
//
// ⚠️ M112:原来是 20 个 case 的 switch,圈复杂度 19(golangci 的 gocyclo 上限是 15)
//
//	—— 拆成两张查表。语义逐条等价,`TestM112_RuleLabel_Coverage` 表驱动守着。
func ruleLabel(rule, param string) string {
	if tpl, ok := ruleLabelsWithParam[rule]; ok {
		if rule == "oneof" {
			// oneof 的参数是空格分隔的候选值,展示时换成 " / " 分隔
			param = strings.ReplaceAll(param, " ", " / ")
		}
		return fmt.Sprintf(tpl, param)
	}
	if s, ok := ruleLabels[rule]; ok {
		return s
	}
	// 未知规则返通用文案
	if param != "" {
		return fmt.Sprintf("不符合 %s(%s) 规则", rule, param)
	}
	return fmt.Sprintf("不符合 %s 规则", rule)
}
