// 熔断器配置(基于 sony/gobreaker)
// 文档 §1.3 #14 P0
package config

// BreakerConfig 全局熔断默认值
//   - Enabled:                 全局开关;false 时所有 Breaker() 中间件透传
//   - ConsecutiveFailures:     连续失败次数阈值,达到则跳闸。
//     **配 0 = 不因连续失败次数跳闸**(不是"立刻跳闸",也不是"没配就跳闸";
//     见 middleware/breaker.go 的 ReadyToTrip 注释)。要关熔断请用 Enabled=false。
//   - Interval:                统计窗口(在该窗口内计数失败)
//   - Timeout:                 跳闸后 open 持续时间(open 期间所有请求直接 fail)
//   - HalfOpenMaxRequests:     open → half-open 后允许放行的探测请求数
//   - FailureRateThreshold:    **预留字段,当前未实现** —— 实现不读它,
//                             熔断只按 ConsecutiveFailures 判定。
//                             配了不生效,启动时会打 WARN(middleware/breaker.go)。
//                             (gobreaker v1.0 默认实现基于 ConsecutiveFailures,
//                              这里预留字段以备切换)
type BreakerConfig struct {
	Enabled              bool    `mapstructure:"enabled"`
	ConsecutiveFailures  uint32  `mapstructure:"consecutive_failures"`
	Interval             int     `mapstructure:"interval_seconds"` // 秒
	Timeout              int     `mapstructure:"timeout_seconds"`  // 秒
	HalfOpenMaxRequests  uint32  `mapstructure:"half_open_max_requests"`
	FailureRateThreshold float64 `mapstructure:"failure_rate_threshold"`
}

// TimeoutConfig 全局超时默认值(per-route 仍可覆盖)
//   - DefaultRequest: 普通请求默认超时(查询/修改等)
//   - LongRequest:    长请求超时(导出/批量导入等)
type TimeoutConfig struct {
	DefaultRequest int `mapstructure:"default_request_ms"` // ms
	LongRequest    int `mapstructure:"long_request_ms"`    // ms
	UploadRequest  int `mapstructure:"upload_request_ms"`  // ms
}
