// OpenTelemetry trace 配置
// 文档 §1.3 #13 + §1.8 #7 P0
package config

// OTelConfig OpenTelemetry trace 导出配置
//   - Enabled:     总开关;false 时 Init 立即 no-op,无任何依赖初始化
//   - Endpoint:    OTLP collector 地址。**只写 host:port**(如 localhost:4317);
//     写不写 http:// 前缀都行(会被剥掉)—— 走 http 还是 https 由 Insecure 决定。
//     protocol=http 时可带 path(如 localhost:4318/v1/traces),不写则用 /v1/traces
//   - Protocol:    "grpc"(默认)/ "http" — 选 OTLP 协议
//   - Insecure:    true 跳过 TLS(开发环境 / 内部 collector)
//   - SampleRatio: 0.0~1.0 采样率;1.0 全采样,0.1 采 10%
//   - ServiceName: resource.service.name(在 trace 平台上的服务名)
type OTelConfig struct {
	Enabled     bool    `mapstructure:"enabled"`
	Endpoint    string  `mapstructure:"endpoint"`
	Protocol    string  `mapstructure:"protocol"` // grpc / http
	Insecure    bool    `mapstructure:"insecure"`
	SampleRatio float64 `mapstructure:"sample_ratio"`
	ServiceName string  `mapstructure:"service_name"`
	// Env: 部署环境标签(dev/staging/prod),写到 resource.deployment.environment
	Env string `mapstructure:"env"`
}
