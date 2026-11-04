// App 推送配置(§1.9 #10 P2)
//
// ⚠️ **provider 为空串 = 不装配推送通道**,理由同 email:
//
//	"配置了但没生效"比"没配"更难查 —— 运维看着 yaml 里有 push 段,
//	以为在推,其实一个推送都没发出去。
//
// ⚠️ **别跟 pkg/push 搞混**:
//   - `pkg/push`    = 服务端 → 浏览器 的实时事件(SSE 传输层)
//   - `pkg/pushx`   = 服务端 → App 客户端 的离线通知(本配置对应这个)
package config

// PushAliyunEMNSConfig 阿里云 EMNS(原 MPush)配置
type PushAliyunEMNSConfig struct {
	AccessKey    string `mapstructure:"access_key"`
	AccessSecret string `mapstructure:"access_secret"`
	// AppKey EMNS 控制台 AppKey
	AppKey string `mapstructure:"app_key"`
	// Region 默认 cn-hangzhou
	Region string `mapstructure:"region"`
	// DefaultAPNSEnv iOS 环境:DEV / PROD(默认 PROD)
	DefaultAPNSEnv string `mapstructure:"default_apns_env"`
}

// PushConfig 推送通道配置
type PushConfig struct {
	// Provider: ""(不装配) / console(仅日志) / aliyun-emns
	Provider string `mapstructure:"provider"`
	// Timeout 单次推送超时秒数(0 = 用 provider 默认 30s)
	Timeout int `mapstructure:"timeout"`
	// TargetMax 单次推送 target 上限(0 = provider 默认 100,EMNS 的硬限制)
	TargetMax int `mapstructure:"target_max"`

	AliyunEMNS PushAliyunEMNSConfig `mapstructure:"aliyun_emns"`
}
