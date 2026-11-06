# asrx —— 实时语音识别(百炼 Paraformer WebSocket)

> 语音识别(ASR,录音转文字)。同一协议两个入口:**实时识别**(边说边出字,
> WebSocket)与 **一句话识别**(短音频整段进去,同步回纯文本)。
> 与 [voicex](voicex.md) 同属百炼语音能力,**共用同一把 api_key / workspace_id**
> (以及同一套"配置中心覆盖"机制)。

## 功能

- **实时识别(`Stream`)**:建连 + `run-task` → 等 `task-started` →
  会话可发音频(`Send`)、可收逐句结果(`Results()`)、可收尾(`Finish`)。
  这是"边说边出字"的底座 —— 后端 WS 代理(`/biz/asr/stream`)直接架在它上面,
  浏览器只见收窄后的"发音频 / 收结果"协议,**api_key 不出服务端**
- **一句话识别(`Once`)**:整段音频按 100ms 块流进去,聚合全部
  `sentence_end=true` 的句子回纯文本 —— 同步一发一收,短音频(<60s)最省事
- **协议纪律**(对齐官方 WebSocket 接入指南):
  - `task-started` 之前不能发音频(协议硬约束,提前发被服务端断连且无可读错误)
    —— `Stream` 返回的会话保证已过这道门
  - 心跳帧(`sentence.heartbeat=true`)包内直接滤掉,上层不见"结果闪空"
  - `result-generated` 的 `sentence_end=false` 是中间结果(灰字),`true` 是终稿
  - `task-failed` 把 `error_code/error_message` 带出来透传,不吞
- **两层超时防 hang**:握手 15s + 整次任务兜底(默认 300s,可配),
  服务端 hang 拖不死调用方

**不解决什么**:

- 不做**录音文件识别**(长录音转写,DashScope 的异步任务那套)—— 后台场景
  暂无此需求,要加时按 `sys_voice_clone` 台账的异步模式起一条新链路;
- 不在包里管鉴权 —— asrx 是纯客户端,HTTP 面的权限闸在路由层
  (`asr:recognize`)与 handler(super_admin 短路)。

## 配置

识别与克隆 / 合成**共用 `voice:` 块的 `api_key` / `workspace_id`**(百炼同一
账号体系),只多两个识别专属键:

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `voice.asr_model` | string | `paraformer-realtime-v2` | 识别模型。Paraformer 仅华北2(北京)地域可用 |
| `voice.asr_timeout_seconds` | int(秒) | `300` | 整次识别任务兜底超时(防 hang 强拆) |

> 识别同样吃配置中心覆盖:`voice.*` 组在「系统配置」面板改 api_key /
> workspace_id,识别与克隆 / 合成一样**改完即生效**(同一份 `ResolveVoiceConfig`)。

## 业务路由(接口面,菜单轮次再接页面)

| 路由 | 权限码 | 说明 |
|---|---|---|
| `POST /biz/asr/recognize` | `asr:recognize` | 一句话识别:multipart 传短音频(`format`/`sampleRate`/`languageHints` 选填),同步回 `{"text":"..."}` |
| `GET /biz/asr/stream` | `asr:stream`(handler 内 CanAccess 判) | 实时识别 WS 代理,握手参数 `?access_token=&format=&sample_rate=&language_hints=`;**豁免 rbac 的替代鉴权**见 consistency.go 注释 |

WS 代理的浏览器侧协议(收窄后的):

```
浏览器 → 服务端:Binary 帧 = 音频块;Text 帧 {"action":"finish"} = 说完了
服务端 → 浏览器:{"event":"sentence", text, begin_time, end_time, end} × N
              → {"event":"finished"} 或 {"event":"failed","message":"..."}
```

## 使用方法

代码侧直接用客户端(片段出自 [examples/asrx/main.go](../../examples/asrx/main.go)):

```go
client, err := asrx.New(asrx.Config{
    APIKey:      cfg.Voice.APIKey,
    WorkspaceID: cfg.Voice.WorkspaceID,
    // URL / Model 留空 = 内置默认:
    //   wss://dashscope.aliyuncs.com/api-ws/v1/inference
    //   paraformer-realtime-v2
})

// 一句话识别:整段音频 → 纯文本
text, err := client.Once(ctx, asrx.RecognizeParams{Format: "wav", SampleRate: 16000}, audioReader)

// 实时识别:会话式,边发边收
sess, err := client.Stream(ctx, asrx.RecognizeParams{Format: "pcm"})
defer sess.Close()
for {
    n, rerr := mic.Read(buf) // 你的音频源
    if n > 0 { _ = sess.Send(buf[:n]) }
    select {
    case sen := <-sess.Results():   // sen.End=false 中间结果 / true 终稿
    case <-sess.Done():             // 终态(finished / failed)
        return
    }
    if rerr == io.EOF { _ = sess.Finish() }
}
```

跑最小示例:

```bash
go run ./examples/asrx
```

## 关键纪律

- **凭据缺一不可**:`asrx.New` 对空 api_key 显式报错(与 voicex 同款);
- **音频必须走 Binary 帧、指令走 Text 帧**:混了协议直接错乱,
  包内 `Send` 只发 Binary、`Finish` 只发 Text,调用方没有犯错的机会;
- **一次会话一套参数**:format / sample_rate 在 run-task 里定死,
  中途要换 = 重新 Stream(WS 代理把参数钉在握手 query 里,同口径);
- **错误必须可见**:task-failed / 连接中断 / 兜底超时都作为显式错误
  交给调用方,不让"识别失败"静默成空文本。
