# oss —— 对象存储统一抽象(本地磁盘 / 阿里云 OSS)

> 文件存哪、key 怎么拼、直链怎么给 —— 一套接口罩住本地磁盘和阿里云 OSS。
> 本仓的模型是**一个桶 / 一棵树 / key 恒 5 段**,**文件没有 public 状态**。

## 功能

### Provider 接口(8 个方法)

| 方法 | 干什么 |
|---|---|
| `Name()` | 实现名(`local` / `oss` / `mock`),排障和选型断言的抓手 |
| `SignUploadToken(key, ttl, maxSize)` | 签发上传凭证 |
| `Put(key, r, contentType)` | 服务端直写(必须是**原子**的) |
| `SignDownloadURL(key, ttl)` | 签出下载 URL |
| `Stat(key)` | 取单个对象元信息 |
| `Delete(keys...)` | 删对象(key 不存在**不算错**) |
| `DeletePrefix(prefix)` | 删前缀下全部对象,**返回删掉的字节数** |
| `List(opt)` | 列举前缀下的对象 + 虚拟目录 |

三个实现:**`local`**(本地磁盘)、**`aliyun`**(阿里云 OSS)、**`MockProvider`**(内存桩)。

### key 恒 5 段

```
<root>/<userID>/<biz>/<folderID>/<objectName>
```

例:`rapid-admin/1001/order/5001/1756000000-a1b2c3d4-报告扫描件.pdf`

- `root` —— 路径根词,默认 `rapid-admin`,可配(`file.path_root`)
- `userID` / `biz` / `folderID` —— **全部由服务端决定**,客户端只能提交文件名
- `objectName` —— `{unix秒}-{rand8}-{清洗后原名}`,扩展名一定留在末尾

### 文件没有 public 状态

存储侧只有**一个桶**(阿里云)或**一棵树**(本地磁盘),**所有文件都能按 URL 直接取到**。
"私密"只意味着**外部用户猜不到这个 key**(uid 是雪花 id、对象名带随机后缀),
而不是"存在另一个别人访问不到的目录"。

所以 key 里**不许有 public 段**,也不许有"公开/私有"两套形态 ——
两套形态意味着两套路径规则、两套前缀清单、两处删除前缀逻辑,而它们迟早会分叉。

### 每个文件都有一个不带签名的稳定直链

前端把这个 URL 直接塞进 `<img src>`。签名 URL 一过期,页面上的图就裂 ——
而文件列表页可能开一整天。直链稳定是刻意的,代价是**桶必须是公共读**。

### 可选接口(高级特性)

`MultipartProvider`(分片 + 断点续传)、`ScanProvider`(病毒扫描)、
`ThumbnailProvider`(缩略图)、`HotlinkProvider`(防盗链)、`QuotaProvider`(配额)、
`CDNProvider`(CDN 刷新/预热)、`ContentProvider`(服务端中转读)、
`MetaReader`(读小元数据对象)。

```go
mp, sp, tp, hp, qp, cp := oss.Advanced(provider)   // 不支持的一律返 nil
```

### 它不解决什么

- 不做业务编排(什么时候上传、配额怎么算、归属怎么校验)—— 那是 service 层
- 不做分片阈值的策略决策(该用多大分片)—— `CheckPartSize` 只管硬限制
- 不做"公开/私有"两套布局(本仓没有这个概念)

## 配置

### yaml 侧(`file:` 块 —— 决定"文件放在哪、怎么放")

```yaml
file:
  provider: local              # local | aliyun。**空 = local**
  root: data/files             # local 时的磁盘根(相对 server/,加载时转绝对)
  path_root: rapid-admin       # 路径根词 = key 的第一段 = 静态挂载点
  allowed_biz: []              # 业务段白名单。**留空 = 不限**
  max_file_size_mb: 100        # 单文件上限,0 = 不限
  multipart_threshold_mb: 100  # 超过这个大小建议走分片
  multipart_part_size_mb: 8    # 分片大小(下限对齐 OSS 的 100KB)
  session_ttl: 24h             # 上传会话有效期
  download_url_ttl: 15m        # 下载 URL 默认有效期
  max_download_ttl: 24h        # 下载 URL 有效期上限
```

### yaml 侧(`oss:` 块 —— 决定"怎么连阿里云")

```yaml
oss:
  endpoint: oss-cn-hangzhou.aliyuncs.com
  endpoint_private: oss-cn-hangzhou-internal.aliyuncs.com
  access_key: ${OSS_ACCESS_KEY}
  access_secret: ${OSS_ACCESS_SECRET}
  bucket: rapid-admin
  cdn_domain: https://cdn.rapid-admin.local
  upload_token_ttl: 3600
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `file.provider` | string | `local` | `local` / `aliyun`。空 = local |
| `file.root` | string | `data/files` | local 的磁盘根 |
| `file.path_root` | string | `rapid-admin` | key 第一段 + 静态挂载点 |
| `file.allowed_biz` | []string | 空(不限) | 业务段白名单,填了就当白名单用 |
| `file.max_file_size_mb` | int | `100` | 单文件上限,0 = 不限 |
| `file.multipart_threshold_mb` | int | `100` | 建议分片的阈值 |
| `file.multipart_part_size_mb` | int | `8` | 分片大小 |
| `file.session_ttl` | duration | `24h` | 上传会话有效期 |
| `file.download_url_ttl` | duration | `15m` | 下载 URL 默认有效期 |
| `file.max_download_ttl` | duration | `24h` | 下载 URL 有效期上限 |
| `oss.endpoint` | string | — | **外网** endpoint,只用来拼直链 |
| `oss.endpoint_private` | string | — | **内网** endpoint,只给 SDK 客户端 |
| `oss.access_key` / `oss.access_secret` | string | — | 阿里云 AK/SK |
| `oss.bucket` | string | — | **只有一个**桶,必须是公共读 |
| `oss.cdn_domain` | string | 空 | 可选 CDN 域名,配了就用它拼直链 |
| `oss.upload_token_ttl` | int(秒) | `3600` | 直传凭证有效期。⚠️ **当前没有接线**(见下) |

⚠️ **`oss:` 整块在 `file.provider: local` 时不生效**;`file:` 块在 `aliyun` 模式下
照样生效(路径形态和配额闸跟后端无关)。两块**正交,别合并**。

### 代码侧

```go
local.New(local.Config{Root: ..., URLBase: ...})            // 本地磁盘
aliyun.New(aliyun.Config{Endpoint: ..., Bucket: ..., ...})   // 阿里云 OSS
oss.NewMockProvider()                                        // 内存桩
```

`local.Config.URLBase` 是静态挂载点(如 `/rapid-admin`),
**必须等于静态挂载点** —— 而挂载点由 `path_root` 推导,两边从同一个值算出来,
就不可能对不齐(以前它们各配各的,对不齐的症状是"URL 永远 404")。

## 使用方法

完整示例:`examples/oss/main.go`。

### 一、对象名清洗

```go
name, err := oss.NewObjectName("报告 扫描件.pdf")   // 1756000000-a1b2c3d4-报告 扫描件.pdf
ext := oss.ObjectExt(name)                           // ".pdf"
```

### 二、构造 key

```go
spec := oss.PathSpec{
    Root:       oss.DefaultRoot,   // "rapid-admin"
    UserID:     1001,
    Biz:        "order",
    FolderID:   5001,
    ObjectName: name,
}
key, err := oss.BuildKey(spec)     // rapid-admin/1001/order/5001/<name>
```

### 三、反解 key

```go
parts, err := oss.ParseKey(oss.DefaultRoot, key)
// parts.Root / parts.UserID / parts.Biz / parts.FolderID / parts.ObjectName
```

### 四、MockProvider(不依赖外部存储)

```go
p := oss.NewMockProvider()
meta, err := p.Put(ctx, key, bytes.NewReader([]byte("hello")), "application/pdf")
got, err := p.Get(ctx, key)
_, err = p.Stat(ctx, "不存在的key")
oss.IsNotFound(err)    // true
```

### 五、列举

```go
res, err := p.List(ctx, oss.ListOption{Prefix: oss.DefaultRoot + "/1001/"})
```

### 六、删前缀(返回字节数)

```go
n, err := p.DeletePrefix(ctx, oss.DefaultRoot+"/1001/")
// 用户存储配额靠这个值回退
```

### 七、直链

```go
url, err := p.SignDownloadURL(ctx, key, time.Hour)
```

## 注意事项

- ⚠️ **key 恒 5 段,段数是死的**:`root/uid/biz/folder/name`。
  `ParseKey` **不许"尽力而为"解析出一半** —— 归属校验宁可失败也不能猜。
  多一段少一段都是非法 key。

- ⚠️ **`ParseKey` 是 key 的唯一反解入口**。全仓只能问这一份实现 ——
  不许在 service / handler / 前端各写一遍 `strings.Split(key, "/")` 再按下标取:
  那种写法在"biz 段数变了"、"root 可配"时会错位,而错位之后**取到的 uid 是别人的**,
  归属校验会静默失效。

- ⚠️ **文件没有 public 状态**。私密靠"key 不可猜"(uid 是雪花 id、对象名带随机后缀),
  不是靠 ACL 挡。key 里不许有 public 段。

- ⚠️ **直链是不带签名、不带过期时间的稳定 URL** ⇒ **桶必须是公共读(public-read)**。
  私有桶上它会 403,症状是"传上去了、列表里点得开、打开是裂图"。
  `SignDownloadURL` 的 `ttl` 参数在这里**不用**(没有过期时间的东西不需要 ttl),
  签名它只是为了满足 Provider 接口的统一形状。

- ⚠️ **CDN 必须真的回源到这个桶**,否则直链全是 404 —— 症状同样是"上传成功、
  列表里点得开、打开是裂图"。

- ⚠️ **直链永远用外网 endpoint**。`oss.endpoint_private` 只给 SDK 客户端用 ——
  直链是给公网上的浏览器看的,给它内网地址它访问不到。
  而且 `endpoint_private` 在构造时会探一次**TCP 连通性**(不只是 DNS:
  阿里云 `-internal` 域名在公网上**也能解析**,指向 VPC 内部的 100.64.0.0/10,
  路由不通),连不上就自动回落外网 endpoint。

- ⚠️ **provider 名有常量,别写字面量**:`oss.ProviderNameLocal` / `oss.ProviderNameOSS` /
  `oss.ProviderNameMock`。service 层要按这个名字判断"文件实际躺在哪"
  (`model.FileStorage`)。早先 service 层比的是手写字符串 `"aliyun"`,而 aliyun
  provider 的 `Name()` 返回 `"oss"` —— **永远不相等**,于是无论真配了 OSS 还是本地磁盘,
  每一行文件都被记成"本地磁盘",逐行记 storage 的意义整个废掉。

- ⚠️ **`oss.provider` 这个 yaml 键不存在**(已删)。真正决定"文件存哪"的是
  `file.provider`。留着它的害处比删掉大:配置里写什么都**零症状**(viper 不认的键直接丢掉),
  于是"这个键到底管什么"永远查不出来。

- ⚠️ **`SignUploadToken` 的参数是完整 key,不是目录前缀**。旧的 PostObject 形态只签
  `starts-with $key =<dir>`,客户端能在那个前缀下**任意**写 key —— 拿到凭证就能把文件
  塞进同 bucket 的任意目录。改成 `eq $key =<key>` 之后,凭证的作用域就是那一个对象。

- ⚠️ **`Stat` 找不到对象时必须让 `oss.IsNotFound(err) == true`** —— service 层靠它
  把 404 和 500 分开。上游用 `errors.Is(err, os.ErrNotExist)` 是判不出来的
  (`ErrNotFound` 实现了 `Is` 方法,`errors.Is` / `errors.As` 都能用)。

- ⚠️ **`DeletePrefix` 返回字节数**:用户存储配额(`sys_user.storage_used_bytes`)靠它回退,
  没有返回值就只能整表重算。而 **`Delete` 里 key 不存在不算错**(幂等),否则重试会永远失败。

- ⚠️ **`SafeKey` 允许 Unicode(中文文件名在本平台是常态),但拒 `..`、`\`、控制字符、
  空段、单段超长**。空段最容易漏:只查 `HasPrefix`/`HasSuffix` 的话 `a//b` 会漏过去,
  而它在 `filepath.Join` 之后会静默塌成 `a/b`。
  ⚠️ **`SafeKey` 不等于安全** —— 它只管字符,不管这个 key 属不属于当前用户,
  归属校验在 service 层,别拿 `SafeKey` 当授权用。

- ⚠️ **对象名带 unix 秒 + rand8 前缀,不是原名直用**。Windows/macOS 的磁盘大小写不敏感:
  `A.pdf` 和 `a.pdf` 在 DB 里是两行,在磁盘上是同一个文件,第二行会**静默覆盖**第一行。

- ⚠️ **`oss.DefaultRoot` 从 `res` 改成 `rapid-admin` 是一次数据迁移**,不是改个常量就完了:
  key 的第一段同时是静态挂载点,改了之后存储上的对象和库里的 key 必须一起搬,
  否则每个历史文件 404(而且直链不经后端,日志里可能一次请求都没有)。
  它必须跟 `file.path_root` 的默认值一致 —— 两者一旦漂移,"用兜底值的路径"和
  "用配置值的路径"会长成两棵树。

- ⚠️ **local 的 `Put` 是原子的**:同目录临时文件 → fsync → rename 覆盖。
  临时文件必须在**同目录** —— 跨文件系统的 rename 会失败(EXDEV),而 `/tmp` 和数据盘
  几乎总是两个文件系统。Windows 上 rename 覆盖一个"正被读"的文件会 EPERM,
  实现里带退避重试(6 次)。

- ⚠️ **local 的路径解析有三层防御**:`SafeKey` → `filepath.Rel` 边界检查 →
  符号链接检查。边界检查**不能用 `strings.Contains(target, root)`** ——
  root=/data/res 时 `/data/res2/x` 也 Contains 成立。符号链接检查的求值对象是
  **最近的已存在祖先目录**,不是目标本身(对还不存在的目标文件调 `EvalSymlinks`
  会拿到 ENOENT,把正常上传全拒掉)。

- ⚠️ **直链要逐段 percent-encode**:整条编码一次会把 `/` 也变成 `%2F`,
  那条路径在 OSS 上根本不存在。不编码的话中文直链在"复制到聊天工具/邮件"这类场景里
  会被截断,空格会让 URL 解析提前结束。

- ⚠️ **MockProvider 是内存桩,不是磁盘桩** —— 进程一重启数据全没。
  它的 `SignDownloadURL` 返回的 URL **访问不到任何东西**:测试里断言
  "拿到 URL 就当能下载"会假绿,要测下载得换 local 或真 OSS。

- ⚠️ **`MetaReader.Get` 有 8MB 上限**(各实现),只用于读小元数据对象(当前唯一用途
  是 `info.json`)。拿它读大文件就是应用服务器 OOM —— 大文件走 `ContentProvider` 或直链。

- ⚠️ **`oss.upload_token_ttl` 配了不生效**。yaml 里的这个值被读进
  `config.OSSConfig.UploadTokenTTL`,但**没有任何地方把它接到
  `service.FileOptions.UploadTokenTTL`** —— service 那边始终用默认值
  (30 分钟)。凭证有效期想改,得走 service 层的选项,不是改 yaml。

- ⚠️ **分片大小不要依赖默认值**。默认 `DefaultPartSize` 是 10MB,对 100GB 文件会
  **正好卡满 10000 片上限**;1GB 文件用 100MB 分片只要 10 片。
  `WithPartSize` / `WithTotalSize` 是 `InitMultipartUpload` 的选项,
  `CheckPartSize` / `CheckPartNumber` 只管硬限制(下限 100KB、上限 10000 片)。

## 相关

- 源码:`internal/pkg/oss/`(`provider.go` 接口 / `path.go` key 与路径 / `upload.go` 上传模式 /
  `advanced.go` 可选能力 / `mock.go` 内存桩),实现见 `internal/pkg/oss/local/` 与
  `internal/pkg/oss/aliyun/`
- 可运行示例:`examples/oss/main.go` —— `go run ./examples/oss`
- 音视频探测与转码(文件落到存储之后的加工)→ [media](media.md)
- 限流与登录锁定(与存储无关的另一类状态)→ [kvstore](kvstore.md)
