// Package jsonid 提供"在 JSON 上必须是字符串"的主键类型。
//
// # 为什么需要它
//
// 全站主键由 sonyflake 生成,是 19 位十进制数(当前量级 ~9.5e16),而 JavaScript
// 的 Number 只能精确表示到 2^53-1(9007199254740991,~9.0e15)。ID 比上限大
// 10.5 倍,超出部分在 JSON.parse 时被静默截断:
//
//	wire 上真实的 id:  94883951113533126
//	JS 解析后变成:     94883951113533120   ← 末 6 位被吃掉
//
// 信息在解析那一刻就没了,**前端侧任何补救都拿不回真值**。把截断后的值回传
// 后端就会 404 —— 身份切换就是死在这里:
// POST /auth/switch-identity {"identity_id": 94883951113533120} → 40400 身份不存在
//
// 这不是前端能修的问题:JS 语言层面就没有 64 位整数。大整数只能靠字符串传。
//
// # 为什么不用 struct tag 的 `,string`
//
// `json:"id,string"` 对标量(字符串/浮点/整数/布尔)有效,但
// **encoding/json 明确忽略数组/切片/结构体上的 string 选项**
// (见标准库文档:"It applies only to fields of string, floating point,
// integer, or boolean types")。本仓库有 identity_ids / department_ids /
// material_ids 这类批量 ID 字段,它们是 []uint64,`string` 帮不上忙。
//
// # 用法
//
//	// 响应 DTO
//	type IdentityView struct {
//	    ID   jsonid.ID    `json:"id"`
//	    Dept jsonid.ID    `json:"dept_id,omitempty"`
//	}
//
//	// service 层从 model 取 uint64
//	view.ID = jsonid.ID(identity.ID)
//
//	// 反方向同理 —— 请求 DTO 也用 jsonid.ID,前端发 {"user_id":"123"}
//	// 注意:如果 DTO 是 uint64 又要能收字符串,tag 上必须有 ,string;
//	// 两个方向不一致就是"登录能过、提交报 400"的经典成因,所以这里统一走类型。
//
// # 兼容性
//
// 存量库里全是 uint64 主键,本包只影响 **JSON 序列化**,不碰 GORM 列类型。
// database/sql / GORM 读写的仍然是 uint64。
package jsonid

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
)

// ID 是"在 JSON 上以字符串表示"的 uint64 主键。
type ID uint64

// IDList 是 ID 的批量形式([]uint64 在 JSON 上必须整体是字符串数组,
// 内置的 `,string` tag 选项管不了它)。
type IDList []ID

// ErrNotString 收到的是 JSON 字符串以外的东西
var ErrNotString = errors.New("jsonid: 值必须是 JSON 字符串")

// MarshalJSON 把 ID 序列化成字符串
func (id ID) MarshalJSON() ([]byte, error) {
	return strconv.AppendQuote(nil, strconv.FormatUint(uint64(id), 10)), nil
}

// UnmarshalJSON 从 JSON 字符串解析 ID
//
// 同时接受裸数字,是为了容忍"某个还没改造的兄弟接口"发来 123。
// 静默兼容看起来像纵容,但拒绝会让整个前端在改造完成前全挂 —— 而改造是
// 分模块推进的,中途必须能跑。改完全部走字符串之后这条路径自然死掉,
// TestID_UnmarshalRejectsNonStringNotApplicable 里记着这件事。
func (id *ID) UnmarshalJSON(b []byte) error {
	s, err := unquote(b)
	if err != nil {
		return err
	}
	if s == "" {
		*id = 0
		return nil
	}
	v, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return fmt.Errorf("jsonid: %q 不是合法 ID: %w", s, err)
	}
	*id = ID(v)
	return nil
}

// MarshalJSON 把 IDList 序列化成字符串数组
func (l IDList) MarshalJSON() ([]byte, error) {
	if l == nil {
		return []byte("null"), nil
	}
	var buf bytes.Buffer
	buf.WriteByte('[')
	for i, id := range l {
		if i > 0 {
			buf.WriteByte(',')
		}
		b, err := id.MarshalJSON()
		if err != nil {
			return nil, err
		}
		buf.Write(b)
	}
	buf.WriteByte(']')
	return buf.Bytes(), nil
}

// UnmarshalJSON 从 JSON 字符串数组解析 IDList
//
// 特殊值:null → nil(而不是空数组),保留"没传"与"传了空数组"的区别。
// 前端据此区分"不过滤"和"过滤到空"。
func (l *IDList) UnmarshalJSON(b []byte) error {
	trimmed := bytes.TrimSpace(b)
	if bytes.Equal(trimmed, []byte("null")) {
		*l = nil
		return nil
	}
	if len(trimmed) == 0 {
		return errors.New("jsonid: IDList 不能是空字节")
	}
	if trimmed[0] != '[' {
		return fmt.Errorf("%w: IDList 必须以 [ 开头", ErrNotString)
	}
	var raw []json.RawMessage
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return err
	}
	out := make(IDList, 0, len(raw))
	for i, item := range raw {
		var id ID
		if err := id.UnmarshalJSON(item); err != nil {
			return fmt.Errorf("IDList[%d]: %w", i, err)
		}
		out = append(out, id)
	}
	*l = out
	return nil
}

// Uint64 取底层值(给 service / GORM 用)
func (id ID) Uint64() uint64 { return uint64(id) }

// Uints 把 IDList 转成 []uint64
func (l IDList) Uints() []uint64 {
	out := make([]uint64, 0, len(l))
	for _, id := range l {
		out = append(out, uint64(id))
	}
	return out
}

// FromUints 从 []uint64 构造 IDList
func FromUints(in []uint64) IDList {
	out := make(IDList, 0, len(in))
	for _, v := range in {
		out = append(out, ID(v))
	}
	return out
}

// FromUint 构造单个 ID
func FromUint(v uint64) ID { return ID(v) }

// unquote 去掉 JSON 字符串的引号;裸数字原样返回
func unquote(b []byte) (string, error) {
	t := bytes.TrimSpace(b)
	if len(t) == 0 {
		return "", errors.New("jsonid: 值为空")
	}
	if t[0] == '"' {
		var s string
		if err := json.Unmarshal(t, &s); err != nil {
			return "", err
		}
		return s, nil
	}
	// 裸数字 —— 兼容改造尚未完成的兄弟接口
	return string(t), nil
}
