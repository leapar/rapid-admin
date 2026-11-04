// 收件人解析 —— UserID → Recipient
package notifyx

import (
	"context"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
)

// Resolver 收件人解析器
//
// ⚠️ **为什么要单独一层**:调用方(比如审批事件投递)手里只有 UserID,
//
//	而通道需要的是联系方式(邮箱 / 手机号)。这个"ID → 联系方式"的翻译
//	放在通道里会让每个通道各查一次库;放在这里则是**查一次,多通道共用**。
type Resolver interface {
	// Resolve 解析收件人。查不到人时返回 error。
	Resolve(ctx context.Context, userID uint64) (Recipient, error)
}

// DBResolver 从 sys_user 读联系方式
type DBResolver struct {
	db *gorm.DB
}

// NewDBResolver 构造
func NewDBResolver(db *gorm.DB) *DBResolver {
	return &DBResolver{db: db}
}

// Resolve 查用户联系方式
//
// 🔴 **email 字段现在是明文,但模型声明它将来要加密** ——
//
//	`internal/service/user_impl.go` 的头注释写着"本期保持明文",
//	而 `model.SysUser.Email` 的声明是 `size:512`(跟加密后的 Phone 一样长)
//	并且字段注释在 Phone 上明确标了 "AES-256-GCM(Base64)"。
//
//	也就是说:**这根引信一直在那儿**,只是还没点着。如果这里直接把
//	库里的值当邮箱发出去,那么等 PII 加密真的落地的那天,
//	平台上会出现"把 Base64 密文当收件地址发邮件"的情况 ——
//	邮件会发到一个不存在的地址,或者被网关拒收,而日志里看不出任何异常。
//
//	所以这里**必须做一次格式校验**:不像邮箱的值一律当作"没有邮箱"。
//	判断"没有"比判断"有"更安全 —— 少发一封邮件的代价,远小于
//	把密文当地址发出去。
func (r *DBResolver) Resolve(ctx context.Context, userID uint64) (Recipient, error) {
	if userID == 0 {
		return Recipient{}, fmt.Errorf("notifyx: userID 为 0")
	}
	if r.db == nil {
		return Recipient{}, fmt.Errorf("notifyx: resolver 没有 db")
	}
	var u model.SysUser
	if err := r.db.WithContext(ctx).
		Select("id", "email", "phone").
		Where("id = ?", userID).
		Take(&u).Error; err != nil {
		return Recipient{}, fmt.Errorf("notifyx: 查用户 %d: %w", userID, err)
	}
	return Recipient{
		UserID: u.ID,
		Email:  normalizeEmail(u.Email),
		Phone:  normalizePhone(u.Phone),
	}, nil
}

// normalizeEmail 取出可用的邮箱,不像邮箱的一律当没有
//
// ⚠️ **判定条件故意很松** —— 只要求"像"一个邮箱:
//
//	含 @、@ 后面有东西、整串没有空白。
//	严格校验 RFC 5322 只会误杀合法地址(比如带中文显示名的地址),
//	而这里要解决的只是"别把密文当地址发出去"。
func normalizeEmail(raw string) string {
	v := strings.TrimSpace(raw)
	if v == "" {
		return ""
	}
	if strings.ContainsAny(v, " \t\r\n") {
		return ""
	}
	at := strings.Index(v, "@")
	if at <= 0 || at == len(v)-1 {
		return ""
	}
	// @ 之后至少要有一个点(域名),且点不能紧挨着 @
	domain := v[at+1:]
	dot := strings.Index(domain, ".")
	if dot <= 0 || dot == len(domain)-1 {
		return ""
	}
	return v
}

// normalizePhone 取手机号
//
// ⚠️ **手机号同样是"本期明文、将来加密"** —— 判定条件:
//
//	11 位、以 1 开头、全数字。加密后是 Base64,长度和形状都对不上。
func normalizePhone(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) != 11 || v[0] != '1' {
		return ""
	}
	for i := 0; i < len(v); i++ {
		if v[i] < '0' || v[i] > '9' {
			return ""
		}
	}
	return v
}
