# desensitize —— 字段脱敏 + 检索 hash 工具

> 文档 §9.3
> 用途 1: 业务 service 层对手机号/姓名/邮箱/身份证 等 PII 做 hash,用于等值检索(不存明文,只存 hash 反查)
> 用途 2: `MaskByRule(field, value)` 按字段名匹配规则脱敏,供完整信息查看 API 用(需短信码 + 写审计)

## 函数清单

| 函数 | 用途 | 示例 |
| --- | --- | --- |
| `Phone(s)` | 手机号脱敏 | `13812345678` → `138****5678` |
| `IDCard(s)` | 身份证脱敏 | `110101199001011234` → `1101**********1234` |
| `Name(s)` | 姓名脱敏 | `张三丰` → `张*丰` |
| `Email(s)` | 邮箱脱敏 | `abc@example.com` → `a**@example.com` |
| `BankCard(s)` | 银行卡脱敏 | `6222600012345678` → `************5678` |
| `ContractNo(s)` | 合同号脱敏 | `HFC2026010100234` → `HFC****234` |
| `PhoneHash(s)` | 手机号 SHA256 检索 hash | 用于 `phone_hash = ?` 精确反查(不存明文) |
| `NameHash(s)` | 姓名检索 hash | 同上 |
| `MaskByRule(field, v)` | 通用字段脱敏 | 按 field 名匹配规则 |

## 业务 service 集成示例

```go
package service

import (
    "context"

    "rapid-admin/internal/model"
    "rapid-admin/internal/pkg/desensitize"
)

type ownerService struct{ db *gorm.DB }

func (s *ownerService) ListOwners(ctx context.Context, q OwnerListQuery) (*model.PageResponse, error) {
    var rows []*model.Owner
    var total int64
    // ... GORM 查询 ...

    // === 框架级:返回前统一脱敏 ===
    views := desensitize.ToOwnerViews(rows)
    return &model.PageResponse{List: views, Total: total}, nil
}

func (s *ownerService) GetOwner(ctx context.Context, id uint64) (*model.Owner, error) {
    var o model.Owner
    if err := s.db.First(&o, id).Error; err != nil { return nil, err }
    return &o, nil // service 层不脱敏,handler 调 ToOwnerView
}
```

## 完整信息查看(需短信码 + 写审计)

```go
func (s *ownerService) ViewFullInfo(ctx context.Context, caller *service.Caller, ownerID uint64, field string) (string, error) {
    // 1. 鉴权:仅 role_admin / role_user_admin / role_delivery_admin / role_property_manager
    // 2. 二次鉴权:短信码(sms scene=view_full_phone)
    // 3. 查 db
    var o model.Owner
    s.db.First(&o, ownerID)
    // 4. 写审计(biz_audit_full_info_view)
    s.audit.RecordView(caller, "owner", ownerID, field)
    // 5. 返回明文
    switch field {
    case "phone":    return crypto.Field().Decrypt(o.Phone)  // 需导入
    case "idCard":   return crypto.Field().Decrypt(o.IdCard)
    default:         return "", errcode.New(errcode.BadRequest, "不支持的字段")
    }
}
```
