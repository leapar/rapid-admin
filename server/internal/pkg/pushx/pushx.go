// Package pushx App 推送 —— 抽象 Provider(§1.9 #10 P2)
//
// 目的:
//   - 统一 App Push 通道(通知 / 自定义消息)
//   - 支持多端:Android / iOS / 鸿蒙 / Web
//   - 支持按 target 维度:device / account / alias / tag
//
// 与 sms/email 的区别:
//   - Push 是异步的(API 返 "已下发",到达率由推送平台决定)
//   - 多 target:单 device / 单 user 多个 device / 群发
//   - 通道重要:iOS APNs / Android 厂商通道(华为/小米/OPPO/vivo)/ 系统通道
//
// 选型:
//   - 阿里云 EMNS(原 MPush):国内多厂商通道 + APNs + 鸿蒙 + FCM 一站式
//   - 个推 / 极光 / 友盟:第三方 SaaS
//   - Console / Mock:dev / 测试
//
// 选型策略(框架层建议):
//   - 国内业主端:阿里云 EMNS(覆盖华为/小米/OPPO/vivo 厂商通道,到达率最高)
//   - 海外业务:FCM / APNs
//   - 测试:Mock
//
// 用法:
//
//	// 1. 选 provider
//	emns, _ := pushx.NewAliyunEMNS(pushx.AliyunConfig{AppKey: "...", AppSecret: "..."})
//	mock := pushx.NewMock()
//
//	// 2. 业务组装 Notification 并 Push
//	_ = emns.Push(ctx, pushx.Notification{
//	    Title: "新消息",
//	    Body:  "您有一条新报修工单",
//	    Targets: []pushx.Target{
//	        {Type: "account", Value: "user_123"},
//	    },
//	})
package pushx

import (
	"context"
	"errors"
)

// Sender 推送下发接口
type Sender interface {
	// Push 推送一条
	//   - 返回 nil:推送平台已接受(不代表用户已收到)
	//   - 返回 non-nil:推送平台拒绝(参数错 / 鉴权失败 / 配额用完)
	Push(ctx context.Context, n Notification) error

	// Provider 返回 provider 名称(用于 audit / 监控)
	Provider() string
}

// Notification 推送内容
type Notification struct {
	// 必填
	Title string // 标题
	Body  string // 内容
	// Targets 至少 1 个 target(API 限制)
	Targets []Target
	// 可选
	SubTitle  string // 副标题(iOS 10+ / Android 某些 ROM)
	Sound     string // 提示音(默认 default)
	Badge     int    // 红点数字(iOS)
	Category  string // iOS UNNotificationCategory
	MutableContent bool // iOS 10+ 通知扩展

	// 自定义数据(客户端拿,JSON 友好)
	Data map[string]string
	// 离线 / 定时推送
	PushTime string // RFC 3339,空 = 立即
	ExpireTime string // RFC 3339,空 = 默认 3 天
	// StoreOffline 离线消息是否保存(默认 true)
	StoreOffline *bool
	// Android 通道
	AndroidChannel string // Android 8+ 通知 channel id
	// Android 高级
	AndroidOpenType  string // 1: app / 2: url / 3: activity
	AndroidOpenURL   string
	AndroidOpenActivity string
	// iOS 高级
	IOSApnsEnv    string // DEV / PROD
	IOSBadgeType  string // -1: 减 / 0: 清 / 1: 加
	// 静默(无通知体,只传 Data)
	Silent bool
}

// Target 推送目标
//
//	Type: account / device / alias / tag
//	Value: 对应值
type Target struct {
	Type  string // account | device | alias | tag | tagAnd | tagNot
	Value string
}

// IsValid 校验
func (n Notification) IsValid() error {
	if n.Title == "" && !n.Silent {
		return errInvalid("title 不能为空(非静默模式)")
	}
	if n.Body == "" && !n.Silent {
		return errInvalid("body 不能为空(非静默模式)")
	}
	if len(n.Targets) == 0 {
		return errInvalid("targets 不能为空")
	}
	for _, t := range n.Targets {
		if t.Type == "" || t.Value == "" {
			return errInvalid("target.type/value 不能为空")
		}
	}
	return nil
}

// ============== Errors ==============

// PushError 推送错误
type PushError struct {
	Provider string
	Code     string
	Message  string
	Retry    bool
}

func (e *PushError) Error() string { return e.Provider + ": " + e.Code + ": " + e.Message }

func errInvalid(msg string) *PushError {
	return &PushError{Code: "invalid", Message: msg, Retry: false}
}

// ErrUnsupported 框架未实现该 provider
var ErrUnsupported = errors.New("pushx: provider not supported")
