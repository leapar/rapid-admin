// OSS 配置
package config

type OSSConfig struct {
	// Endpoint **外网** endpoint,如 `oss-cn-shenzhen.aliyuncs.com`。
	//
	// ⚠️ **它只用来拼直链**(给浏览器看)+ 作为 SDK 客户端的回落 endpoint。
	// 内网 endpoint 见 EndpointPrivate。
	//
	// ⚠️ 带不带 scheme 都认:`https://` 前缀会被剥掉再拼(见 oss/aliyun 的
	// normalizeEndpoint),不先规整就会拼出 `https://bucket.https://oss-...`
	// 这种废 URL —— 每个链接都坏,报的还是"域名解析失败",指向不了真因。
	Endpoint string `mapstructure:"endpoint"`
	// EndpointPrivate **内网** endpoint,如 `oss-cn-shenzhen-internal.aliyuncs.com`。
	//
	// 同一地域的阿里云 ECS 上用它更快(流量不走公网)。**只给 SDK 客户端用**,
	// 不参与拼直链 —— 直链是给公网上的浏览器看的,给它内网地址它访问不到。
	//
	// ⚠️ **填了不代表一定用得上**:开发机 / 非阿里云的机器连不上
	// `-internal` 域名(它能解析,但路由不通)。所以构造时会先探一次
	// **TCP 连通性**,连不上就自动回落到外网 endpoint,并把选了哪个打进启动日志。
	//
	// ⚠️ **它是可选的**,不配就是外网直连 —— 配错了的代价只是"没变快",
	// 而硬用连不上的地址的代价是"启动即 Fatal"。
	EndpointPrivate string `mapstructure:"endpoint_private"`
	AccessKey       string `mapstructure:"access_key"`
	AccessSecret    string `mapstructure:"access_secret"`
	// Bucket 存文件的桶 —— **只有一个**。
	//
	// ⚠️ 以前是 bucket_admin + bucket_public 双桶(按 key 前缀分流)。
	// 现在没有"公开/私有"两套布局,单桶 + 公共读即可(见 oss/aliyun 的
	// SignDownloadURL 注释)。
	//
	// ⚠️ **这个桶必须是公共读(public-read)**:直链是拼出来的、不带签名的
	// 稳定 URL,私有桶上它会 403。
	Bucket string `mapstructure:"bucket"`
	// CDNDomain 可选的 CDN 域名。配了就用它拼直链,不配就用 bucket endpoint。
	//
	// ⚠️ **CDN 必须真的回源到这个桶**,否则直链全是 404(症状是"上传成功、
	// 列表里点得开、打开是裂图")。
	CDNDomain      string `mapstructure:"cdn_domain"`
	UploadTokenTTL int    `mapstructure:"upload_token_ttl"` // seconds
}

// ⚠️ **这里没有 `provider` 键**。
//
// M137 之前有 `oss.provider`(dev 里写着 `mock`),而**全仓没有任何代码读它**
// —— 真正决定"文件存哪"的是 `file.provider`(local | aliyun)。两个键讲的是
// 一件事、只有一个有人读,于是 `oss.provider: mock` 让人以为"OSS 是假的、
// 还没接",而实际上 aliyun provider 早就能用。
//
// 留着它的害处比删掉大:配置里写什么都**零症状**(viper 不认的键直接丢掉),
// 于是"这个键到底管什么"永远查不出来。已删,并在 `internal/config/file_test.go`
// 的 removedKeys 列表里盯着它别再回来。
