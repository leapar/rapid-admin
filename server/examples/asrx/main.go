// asrx —— 百炼(DashScope)实时语音识别(Paraformer)WebSocket 客户端
//
// ## 这个包解决什么问题
//
//   - **实时识别(Stream)**:建连 + run-task 后拿回会话,调用方持续
//     Send 音频块、从 Results() 收逐句结果(中间结果 + 终稿)——
//     "边说边出字"的底座,后端 WS 代理直接架在它上面
//   - **一句话识别(Once)**:整段音频流进去,聚合全部终稿句子回纯文本,
//     同步一发一收,短音频(<60s)最省事
//
// ## 关键点
//
//   - **凭据缺一不可**:api_key 为空 New() 直接报错(与 voicex 同款纪律)
//   - **task-started 之前不能发音频**:协议硬约束,提前发被服务端断连
//     且没有可读错误 —— Stream 返回的会话保证已过这道门
//   - **心跳帧不是结果**:sentence.heartbeat=true 的帧包内直接滤掉,
//     上层不会看到"识别结果闪空"
//   - **两层超时**:握手 15s + 整次任务兜底(默认 300s),服务端 hang
//     拖不死调用方
//
// ## 跑法
//
//	go run ./examples/asrx
//
// ⚠️ 演示只做**离线部分**(装配校验 + 协议帧预览)—— 真识别要真凭据真音频,
//
//	由业务层 /biz/asr/* 装配;别在示例里硬编码真 key。
package main

import (
	"encoding/json"
	"fmt"

	"rapid-admin/internal/pkg/asrx"
)

func main() {
	// ── 一、装配期校验:没配就是没配,不当场兜底 ───────────────────
	fmt.Println("=== 装配期校验(缺凭据必须当场报错)===")
	if _, err := asrx.New(asrx.Config{}); err != nil {
		fmt.Println("空配置 New:", err)
	}

	// ── 二、正常构造 + 协议帧预览(演示用假凭据,不发起真实请求)────
	fmt.Println()
	fmt.Println("=== 正常构造(假凭据,展示协议形态)===")
	// ⚠️ 真实凭据放 configs/config.yaml 的 voice 段(api_key / workspace_id),
	//    由业务层解析后注入;asrx 自己不读全局配置。
	client, err := asrx.New(asrx.Config{
		APIKey:      "sk-demo",
		WorkspaceID: "ws-demo",
		// URL / Model 留空 = 内置默认:
		//   wss://dashscope.aliyuncs.com/api-ws/v1/inference
		//   paraformer-realtime-v2
	})
	if err != nil {
		fmt.Println("New:", err)
		return
	}
	fmt.Printf("构造成功:模型默认 %s\n", "paraformer-realtime-v2")

	// run-task 帧(客户端 → 服务端的第一条 Text 帧)
	runTask := map[string]any{
		"header": map[string]any{"action": "run-task", "task_id": "<uuid>", "streaming": "duplex"},
		"payload": map[string]any{
			"task_group": "audio", "task": "asr", "function": "recognition",
			"model":      "paraformer-realtime-v2",
			"parameters": map[string]any{"format": "pcm", "sample_rate": 16000},
			"input":      map[string]any{},
		},
	}
	b, _ := json.MarshalIndent(runTask, "", "  ")
	fmt.Println("run-task 帧形态:")
	fmt.Println(string(b))

	// ── 三、一句话识别的音频分块粒度 ───────────────────────────────
	fmt.Println()
	fmt.Println("=== 音频分块(16kHz 16bit mono)===")
	fmt.Printf("ChunkSize = %dB = 100ms;一段 8s 音频切成 %d 块流进去\n",
		asrx.ChunkSize, 8*16000*2/asrx.ChunkSize)

	// ── 四、结果形态(服务端 → 客户端,包内已滤心跳)────────────────
	fmt.Println()
	fmt.Println("=== 结果解析规则 ===")
	fmt.Println(`sentence_end=false → 中间结果(边说边出的那个"灰字")`)
	fmt.Println(`sentence_end=true  → 终稿;Once() 只聚合终稿回纯文本`)
	fmt.Println(`heartbeat=true     → 心跳帧,包内滤掉,上层不可见`)
	fmt.Println(`task-failed        → 错误带 error_code/error_message 透传给调用方`)

	// 假凭据客户端到此为止:真请求见 internal/service/asr.go 的装配
	_ = client
}
