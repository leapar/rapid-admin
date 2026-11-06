# voicex —— 语音克隆与合成(百炼 CosyVoice + Edge 朗读)

> 两件事:**音色克隆**(把一段参考音频注册成可复用音色,百炼 CosyVoice)
> 和 **文本合成**(文字 → 音频,整段 / 流式两套形态)。
> 合成有两条通道:**百炼**(走可配置的合成端点)与 **Edge 朗读**(微软,免凭据)。
> 语音识别(ASR)是姊妹包 [asrx](asrx.md),与它共用凭据与配置中心覆盖。

## 功能

- **DashScope 音色克隆客户端**:单端点 + action 切换
  (`POST https://{workspace_id}.{region}.maas.aliyuncs.com/api/v1/services/audio/tts/customization`),
  覆盖 create / list / query / update / delete 全套动作。
  ⚠️ **只接 CosyVoice**(`voice-enrollment`)—— Qwen-TTS / Qwen-Audio 的
  action / model / 字段形态都不一样,且产品上不用,刻意没移植
- **文本合成 · 百炼通道**:POST JSON `{text, voice, tts, volume, rate, format}` 到
  `voice.synth_url` 指向的 TTS 网关,**不用 WebSocket** —— 单个 HTTP 请求,
  两套响应形态按需挑:
  - **整段**(`Once`):合成完一次性回完整 wav/mp3 字节 —— 下载 / 存档用
  - **流式**(`OnceStream`):chunked 逐块读 —— 边合成边转发,
    前端 `fetch` + `ReadableStream` 边收边播,首包即首音频帧
- **文本合成 · Edge 通道**(`EdgeClient`):微软 Edge 朗读,**免凭据**。
  协议本身只有 WebSocket 一条通道,那是**对接上游的实现细节** ——
  对外暴露的仍是 `Once` / `OnceStream`,与百炼通道同构,前端不感知 WS:
  - 主握手 `api.msedgeservices.com`(配 `voice.edge.token`)+
    备用握手 Bing 公共 TrustedClientToken,主备自动切换
  - Sec-MS-GEC 风控参数本地计算,403 时按响应 Date 头自动校时
  - 两层超时防 hang:单次 read 30s + 整次任务 180s 兜底强拆
  - 输出固定 mp3(24kHz 48kbit mono),`format=wav` 显式拒绝
- **内置音色目录**:百炼 CosyVoice 官方音色(v2 / v3-flash 全量)+ Edge 常用
  Neural 音色,seed 首启批量登记进 `sys_tts_voice`(**只补缺不覆盖**,
  运营改过名/排序的行不会被重跑 migrate 打回去)
- **"响应是 JSON"= 失败** 的嗅探(百炼通道):部分网关失败时回 200 + `{"code","msg"}`,
  客户端按 Content-Type + 首字节两级嗅探,不让一段 JSON 冒充 mp3 存下去
- **业务层配套**(`internal/service/voice.go`,不在本包):
  `sys_voice_clone` 克隆台账 + `sys_tts_voice` 音色登记册 + `/biz/voice-clones`、
  `/biz/tts/*` 路由(权限码 `voice_clone:*` / `tts:*` / `tts_voice:*`)

**不解决什么**:

- 不做 ASR(语音识别)—— 源项目里 ASR 只有配置透传,没有本地代码;
- 百炼不内置合成后端 —— `synth_url` 为空 = 百炼合成不装配,调用拿到显式报错
  (Edge 通道无凭据依赖,不受此限);
- 不管凭据轮转 —— api_key 就在 yaml 里,和 OSS / SMTP 同一套纪律。

## 配置

### yaml 侧(`voice:` 块)

```yaml
voice:
  api_key: ${VOICE_BAILIAN_API_KEY}
  workspace_id: ""            # 百炼业务空间 ID(拼进端点域名)
  region: cn-beijing          # 端点地域
  default_model: voice-enrollment       # CosyVoice 音色注册(仅此一种)
  default_target_model: cosyvoice-v3-plus
  timeout_seconds: 120        # 克隆接口单次 HTTP 超时(审核链路偏慢)
  synth_url: ""               # 百炼文本合成端点,如 https://tts.example.com/ai/tts/once
  synth_timeout_seconds: 60
  edge:                       # Edge 朗读(免凭据,全可空)
    token: ""                 # 主握手 Ocp-Apim-Subscription-Key(空则走 Bing 公共通道)
    wss_url: ""               # 备用握手端点(空 = 内置 Bing 默认;自建代理时指过来)
    read_timeout_seconds: 30
    total_timeout_seconds: 180
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `voice.api_key` | string | 空 | 百炼 API Key。**为空 = 克隆接口调用时显式报错**(花钱的外呼,失败必须可见) |
| `voice.workspace_id` | string | 空 | 业务空间 ID,拼进端点域名,**必填**(缺了 `New()` 直接报错) |
| `voice.region` | string | `cn-beijing` | 端点地域 |
| `voice.default_model` | string | `voice-enrollment` | 克隆 model(CosyVoice 专用) |
| `voice.default_target_model` | string | `cosyvoice-v3-plus` | 克隆目标音色模型 |
| `voice.timeout_seconds` | int(秒) | `120` | 克隆接口 HTTP 超时(远端审核链路偏慢,别配太小) |
| `voice.synth_url` | string | 空 | 百炼 TTS 网关端点。**空 = 百炼合成不装配**(调用显式报错,与 email.provider 空串同一纪律) |
| `voice.synth_timeout_seconds` | int(秒) | `60` | 百炼合成单次 HTTP 超时 |
| `voice.edge.token` | string | 空 | Azure 语音订阅密钥(主握手)。**不填 = 走 Bing 公共通道,零凭据可用**;填了先走微软官方主端点,握手失败自动落回公共通道 |
| `voice.edge.wss_url` | string | 空 | 备用/代理握手端点。空 = 内置 Bing 默认;公共通道被风控或出网不稳时,指到自建 edge-tts 代理 |
| `voice.edge.read_timeout_seconds` | int(秒) | `30` | 单次 read 超时(卡死检测),默认即可 |
| `voice.edge.total_timeout_seconds` | int(秒) | `180` | 整次合成兜底超时,默认即可 |

> **edge 怎么配,一句话:什么都不填就能用。** 日常留空走 Bing 公共通道
> (Edge 浏览器内置的公共 TrustedClientToken,免凭据);公共通道被微软风控
> (403 / 空音频)时,要么填 `token` 换官方主端点,要么填 `wss_url` 过自建代理,
> 二者都不用动超时参数。

### 配置中心(系统配置面板)

百炼相关的 5 个键(`voice.api_key` / `workspace_id` / `region` /
`default_target_model` / `synth_url`)已 seed 进**系统配置面板**
(`sys_config`,`voice` 分组):配置文件打底,面板覆盖,**改完即生效,不用重启**
—— 克隆 / 合成每次调用时都重新解析生效配置(`service.ResolveVoiceConfig`,
消费方 `voiceCloneService` / `ttsVoiceService`)。

合并规则与 `aliyun.sms.*` 完全一致:**面板值生效 ⇔ 该行非停用 且 值非空
且 值 ≠ 该行自带的默认值**(即"运营改过的才算数");**清空某一格 = 退回
配置文件**。edge 通道免凭据,不进面板;超时参数是运维调优项,也留在配置文件。

### 业务路由(移植后自带)

| 路由 | 权限码 | 说明 |
|---|---|---|
| `POST /biz/voice-clones` | `voice_clone:create` | multipart 上传参考音频 → 克隆(失败也落一条 FAILED 台账) |
| `GET /biz/voice-clones` | `voice_clone:list` | 台账分页(keyword/status/tts_type) |
| `GET /biz/voice-clones/:id` | `voice_clone:read` | 单条 |
| `PATCH /biz/voice-clones/:id` | `voice_clone:update` | **只改展示信息**,不重新训练 |
| `DELETE /biz/voice-clones/:id` | `voice_clone:delete` | 远端音色 + 参考音频尽力删,失败不阻断 |
| `POST /biz/voice-clones/batch-delete` | `voice_clone:delete` | 批量(`{"ids":["1","2"]}`) |
| `POST /biz/voice-clones/:id/refresh` | `voice_clone:refresh` | 拉 `query_voice` 同步状态;终态的 error_msg 不被网络错误覆盖 |
| `GET /biz/voice-clones/remote` | `voice_clone:remote_list` | 远端音色列表(不写库,对账用) |
| `POST /biz/tts/convert` | `tts:convert` | 文本合成:`stream=false` 整段 / `stream=true` 流式;`dir` 非空落 OSS;`provider` 选 `bailian`(默认)/ `edge` |
| `GET /biz/tts/voices` · `POST` · `PATCH/:id` · `DELETE/:id` · `POST batch-delete` | `tts_voice:*` | 音色登记册 CRUD(创建/更新后异步合成试听音频;`tts_type=edge` 的行走 Edge 通道合成试听) |

### 数据范围接线

`voice_clone` / `tts_voice` / `tts` 登记在 `bizScopeUnwired`(平台级资产,
按 API 码 all-or-nothing 管,同 `wxtemplate`,不做行级范围)。

## 使用方法

### 代码侧直接用客户端

```go
client, err := voicex.New(voicex.Config{
    APIKey:      cfg.Voice.APIKey,
    WorkspaceID: cfg.Voice.WorkspaceID,
    Region:      cfg.Voice.Region,
})
// 克隆:参考音频必须是 https:// 公网可访问 URL(DashScope 服务端会回头 GET)
voiceID, err := client.CreateVoiceFromURL("cosyvoice-v3-plus", "voice", audioURL, []string{"zh"})
// 查状态:DEPLOYING → OK / UNDEPLOYED
info, err := client.QueryVoice(voiceID)

// 整段合成(百炼通道)
synth, _ := voicex.NewSynthesizer(voicex.SynthConfig{Endpoint: cfg.Voice.SynthURL})
audio, err := synth.Once(voicex.SynthParams{Text: "你好", Voice: voiceID, Format: "wav"})

// 流式合成(边合成边转发)
stream, err := synth.OnceStream(voicex.SynthParams{Text: "你好", Voice: voiceID})
defer stream.Body.Close()
io.Copy(w, stream.Body)

// Edge 通道(免凭据,同一套 Once / OnceStream 形态)
edge := voicex.NewEdgeClient(voicex.EdgeConfig{})
audio, err = edge.Once(voicex.EdgeSynthParams{Text: "你好", Voice: "zh-CN-XiaoxiaoNeural"})
```

### 内置音色从哪来

`go run ./cmd/migrate` 时 seed 自动把内置音色登记进 `sys_tts_voice`:

- **bailian**:CosyVoice 官方内置(v2 / v3-flash 全量,`voice_id` 即官方 voiceId,
  model 记在 description);Name 用 voiceId 占位,运营可改
- **edge**:常用 Neural 音色(晓晓/云希/方言/粤语/英语/日语/韩语等),
  `voice_id` 是短名(`zh-CN-XiaoxiaoNeural`),合成时 `NormalizeEdgeVoice`
  自动展开成协议全名

幂等纪律:按 `(tts_type, voice_id)` 判存在(Unscoped,软删行也算),
**只补缺不覆盖** —— 运营的改名/排序不会被重跑 migrate 打回去。

### 参考音频的公网 URL 从哪来

克隆要求 **https:// 公网可访问** 的音频 URL。业务层的做法:参考音频先经
`oss.Provider.Put` 进对象存储,再 `SignDownloadURL` 签直链 ——
**key 第一段必须是静态挂载点**(`file.path_root`,装配时拼
`pathRoot + "/voice"` 传入 service),否则 local provider 的直链 404。
local / 无 https 部署下克隆会在 DashScope 侧失败,错误落进台账的 `error_msg`。

## 关键纪律

- **凭据缺一不可**(克隆):`voicex.New` 对 api_key / workspace_id 逐个显式报错 ——
  半可用的客户端会把"没配"拖到第一次调用才炸;
- **https 闸门在客户端这边**:`http://` 或相对路径的参考音频不发请求直接拒,
  不用等远端 403 再猜;
- **合成端点失败必须可见**(百炼通道):200 + JSON 的"假成功"被两级嗅探拦下
  (Content-Type → 首字节),否则一段 JSON 存成 .mp3,播放器只会报"文件损坏";
- **Edge 空 audio 也是失败**:握手被风控但连接没断时,WS 会"正常"收 0 字节 ——
  `Once` 对 0 字节显式报错,不把空 mp3 当成功交出去;
- **只接 CosyVoice**:Qwen-TTS / Qwen-Audio 相关的 action / model / 字段
  刻意没移植,别因为" DashScope 有的都接"而加回来。
