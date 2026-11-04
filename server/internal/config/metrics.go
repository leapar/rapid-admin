// Prometheus metrics 配置
// 文档 §1.8 #8 P0
package config

// MetricsConfig Prometheus 指标配置
//   - Enabled:   总开关;false 时 Init 立即 no-op,/metrics 不挂载
//   - Path:      指标暴露路径,默认 /metrics
//   - Namespace: 指标前缀(默认 app,最终指标名 app_http_requests_total)
//   - Subsystem: 指标子系统(留空则不附加;典型值 http / process / go)
type MetricsConfig struct {
	Enabled   bool   `mapstructure:"enabled"`
	Path      string `mapstructure:"path"`
	Namespace string `mapstructure:"namespace"`
	Subsystem string `mapstructure:"subsystem"`
}
