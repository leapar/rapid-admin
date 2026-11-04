// KMS 配置
// 文档 §1.5 #10 P1 密钥管理服务抽象
//
// 用法:
//   kms:
//     provider: local        # local(默认) / aliyun / tencent / vault
//     endpoint: ""           # aliyun/tencent 专用
//     access_key: ""         # 凭证
//     secret_key: ""         # 凭证
//     region: ""             # 区域(aliyun:cn-hangzhou 等)
//     key_prefix: ""         # 业务前缀(隔离多业务)
//
// 也可用环境变量覆盖(viper AutomaticEnv):
//   KMS_PROVIDER / KMS_ENDPOINT / KMS_ACCESS_KEY / KMS_SECRET_KEY / KMS_REGION / KMS_KEY_PREFIX
package config

// KMSConfig KMS 启动配置
type KMSConfig struct {
	Provider  string `mapstructure:"provider"`
	Endpoint  string `mapstructure:"endpoint"`
	AccessKey string `mapstructure:"access_key"`
	SecretKey string `mapstructure:"secret_key"`
	Region    string `mapstructure:"region"`
	KeyPrefix string `mapstructure:"key_prefix"`
}
