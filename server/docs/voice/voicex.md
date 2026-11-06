# voicex —— 语音克隆与合成(DashScope 音色 + TTS)

> 两件事,共用一份百炼凭据:**音色克隆**(把一段参考音频注册成可复用音色)
> 和 **文本合成**(文字 → 音频,整段 / 流式两套形态)。
> 合成本体不在包里 —— 走可配置的合成端点,换后端不用改代码。

## 功能

- **DashScope 音色克隆客户端**:单端点 + model/action 切换
  (`POST https://{workspace_id}.{region}.maas.aliyuncs.com/api/v1/services/audio/tts/customization`),
  覆盖 create / list / query / update / delete 全套动作,
  CosyVoice(Qwen-Audio-TTS)与 Qwen-TTS 两种返回形态都归一(`RemoteVoice.LocalID()`)
- **文本合成客户端**:POST JSON `{text, voice, tts, volume, rate, format}` 到
  `voice.synth_url` 指向的 TTS 网关,**不用 WebSocket** —— 单个 HTTP 请求,
  两套响应形态按需挑:
  - **整段**(`Once`):合成完一次性回完整 wav/mp3 字节 —— 下载 / 存档用
  - **流式**(`OnceStream`):chunked 逐块读 —— 边合成边转发,
    前端 `fetch` + `ReadableStream` 边收边播,首包即首音频帧
- **"响应是 JSON"= 失败** 的嗅探:部分网关失败时回 200 + `{"code","msg"}`,
  客户端按 Content-Type + 首字节两级嗅探,不让一段 JSON 冒充 mp3 存下去
- **业务层配套**(`internal/service/voice.go`,不在本包):
  `sys_voice_clone` 克隆台账 + `sys_tts_voice` 音色登记册 + `/biz/voice-clones`、
  `/biz/tts/*` 路由(权限码 `voice_clone:*` / `tts:*` / `tts_voice:*`)

**不解决什么**:

- 不做 ASR(语音识别)—— 源项目里 ASR 只有配置透传,没有本地代码;
- 不内置合成后端 —— `synth_url` 为空 = 合成不装配,调用拿到显式报错;
- 不管凭据轮转 —— api_key 就在 yaml 里,和 OSS / SMTP 同一套纪律。

## 配置

### yaml 侧(`voice:` 块)

```yaml
voice:
  api_key: ${VOICE_BAILIAN_API_KEY}
  workspace_id: ""            # 百炼业务空间 ID(拼进端点域名)
  region: cn-beijing          # 端点地域
  default_model: voice-enrollment       # 配 qwen-voice-enrollment 走 Qwen-TTS 克隆
  default_target_model: cosyvoice-v3-plus
  timeout_seconds: 120        # 克隆接口单次 HTTP 超时(审核链路偏慢)
  synth_url: ""               # 文本合成端点,如 https://tts.example.com/ai/tts/once
  synth_timeout_seconds: 60
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `voice.api_key` | string | 空 | 百炼 API Key。**为空 = 克隆接口调用时显式报错**(花钱的外呼,失败必须可见) |
| `voice.workspace_id` | string | 空 | 业务空间 ID,拼进端点域名,**必填**(缺了 `New()` 直接报错) |
| `voice.region` | string | `cn-beijing` | 端点地域 |
| `voice.default_model` | string | `voice-enrollment` | 克隆 model;配 `qwen-voice-enrollment` 时 `CreateVoiceFromURL` 走 Qwen-TTS 路径 |
| `voice.default_target_model` | string | `cosyvoice-v3-plus` | 克隆目标音色模型 |
| `voice.timeout_seconds` | int(秒) | `120` | 克隆接口 HTTP 超时(远端审核链路偏慢,别配太小) |
| `voice.synth_url` | string | 空 | TTS 网关端点。**空 = 合成不装配**(调用显式报错,与 email.provider 空串同一纪律) |
| `voice.synth_timeout_seconds` | int(秒) | `60` | 合成单次 HTTP 超时 |

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
| `POST /biz/tts/convert` | `tts:convert` | 文本合成:`stream=false` 整段 / `stream=true` 流式;`dir` 非空落 OSS |
| `GET /biz/tts/voices` · `POST` · `PATCH/:id` · `DELETE/:id` · `POST batch-delete` | `tts_voice:*` | 音色登记册 CRUD(创建/更新后异步合成试听音频) |

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

// 整段合成
synth, _ := voicex.NewSynthesizer(voicex.SynthConfig{Endpoint: cfg.Voice.SynthURL})
audio, err := synth.Once(voicex.SynthParams{Text: "你好", Voice: voiceID, Format: "wav"})

// 流式合成(边合成边转发)
stream, err := synth.OnceStream(voicex.SynthParams{Text: "你好", Voice: voiceID})
defer stream.Body.Close()
io.Copy(w, stream.Body)
```

### 参考音频的公网 URL 从哪来

克隆要求 **https:// 公网可访问** 的音频 URL。业务层的做法:参考音频先经
`oss.Provider.Put` 进对象存储,再 `SignDownloadURL` 签直链 ——
**key 第一段必须是静态挂载点**(`file.path_root`,装配时拼
`pathRoot + "/voice"` 传入 service),否则 local provider 的直链 404。
local / 无 https 部署下克隆会在 DashScope 侧失败,错误落进台账的 `error_msg`。

## 关键纪律

- **凭据缺一不可**:`voicex.New` 对 api_key / workspace_id 逐个显式报错 ——
  半可用的客户端会把"没配"拖到第一次调用才炸;
- **https 闸门在客户端这边**:`http://` 或相对路径的参考音频不发请求直接拒,
  不用等远端 403 再猜;
- **合成端点失败必须可见**:200 + JSON 的"假成功"被两级嗅探拦下
  (Content-Type → 首字节),否则一段 JSON 存成 .mp3,播放器只会报"文件损坏"。
