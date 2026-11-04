// 字段加密 key(AES-256-GCM + HMAC-SHA256 盲索引)
package config

// CryptoConfig 字段级加密 & 盲索引密钥
//   - FieldKey:     AES-256-GCM 密钥(32 字节 base64,生产由 K8s Secret 注入)
//   - BlindKey:     HMAC-SHA256 盲索引密钥(>=16 字节,生产由 K8s Secret 注入)
type CryptoConfig struct {
	FieldKey string `mapstructure:"field_key"`
	BlindKey string `mapstructure:"blind_key"`
}

type RateLimitConfig struct {
	GlobalQPS        int `mapstructure:"global_qps"`
	PerIPQPS         int `mapstructure:"per_ip_qps"`
	LoginFailPerHour int `mapstructure:"login_fail_per_hour"`
}

type AuditConfig struct {
	BatchSize     int `mapstructure:"batch_size"`
	FlushInterval int `mapstructure:"flush_interval"` // ms
	QueueSize     int `mapstructure:"queue_size"`
}

