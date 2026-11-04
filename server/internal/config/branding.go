// 品牌信息配置 —— 平台显示名(开源通用化)
//
// 为什么单独一个块,而不是塞进 system:
// `system.app_name` 是**服务标识**(OTel service name / buildinfo / 日志前缀),
// 改它会动到可观测性的维度;而品牌名是**给人看的**。
// 两者语义不同,混在一起迟早有人为了改显示名去动服务标识。
//
// 为什么放 yaml 而不是 sys_config(平台配置中心):
// 品牌名要在**登录之前**就拿到(登录页标题就是它),而 sys_config 的读接口
// 挂在 Auth() 下且只绑 super_admin —— 未登录时根本读不到。
// ⇒ 它是"启动期常量",不是"运营期可改项"。
//
// 两个字段都可空,空的表示"没配":
// 前端拿到空串会**回退到本地语言包**(中文界面显示中文名,英文界面显示英文名),
// 这比在服务端给一个中文默认值更好 —— 通用平台不该把中文写死成默认。
//
// ⚠️ 这里**只放有消费方的字段**:语言包里 `layout.brandShort` / `layout.copyright`
// 目前零渲染(2026-11-01 全仓 grep 无调用方),所以服务端也不给它们开字段 ——
// 给了就是"零调用方的导出字段",改一个名字要动三处却只有两处生效。
package config

// BrandingConfig 品牌展示信息 —— 只给人看,不含任何密钥
type BrandingConfig struct {
	// AppName 平台全名(登录页标题 / 顶栏品牌名)。空 = 前端用语言包兜底
	AppName string `mapstructure:"app_name"`
	// AppTitle 浏览器标签页标题。空 = 用 AppName;再空 = 前端用语言包兜底
	AppTitle string `mapstructure:"app_title"`
}

// DisplayName 返回用于界面展示的平台名(空 = 没配,调用方自行兜底)
func (b BrandingConfig) DisplayName() string { return b.AppName }

// TabTitle 返回浏览器标签页标题。
// 派生顺序:app_title → app_name → 空(前端用语言包兜底)。
// ⚠️ 不用 `system.app_name` 兜底 —— 那是**服务标识**(rapid-admin),
// 出现在浏览器标签页上会让使用者以为在访问一个内部服务而不是产品。
func (b BrandingConfig) TabTitle() string {
	if b.AppTitle != "" {
		return b.AppTitle
	}
	return b.AppName
}
