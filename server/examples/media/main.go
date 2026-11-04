// media —— 音视频处理
//
// ## 这个包解决什么问题
//
// 用户上传的视频/音频要能在线预览。但不是所有格式浏览器都能播 ——
// 需要探测、判定、转码。
//
// ## 关键点
//
//   - **判不出来 = 放行**。Info 里任何一项为空(比如 ffprobe 没吐 format_name),
//     BrowserPlayable 就不拦。**别**把"空"当成"浏览器一定不能播"
//   - 转码参数是**显式列出来的**(TranscodeArgs),不是拼字符串 ——
//     拼字符串意味着参数来自文件名时会有注入面
//   - Kind 由文件名 + Content-Type 判定,判定不了就是 KindUnknown
//
// ## 跑法
//
//	go run ./examples/media
//
// ⚠️ 真正转码需要本机装 ffmpeg / ffprobe。本示例只演示**纯函数**部分。
package main

import (
	"fmt"

	"rapid-admin/internal/pkg/media"
)

func main() {
	// ── 一、种类判定 ────────────────────────────────────────────
	fmt.Println("=== 种类判定 ===")
	cases := []struct {
		fileName    string
		contentType string
	}{
		{"交付现场.mp4", "video/mp4"},
		{"会议录音.m4a", "audio/mp4"},
		{"说明.pdf", "application/pdf"},
		{"录像.avi", "video/x-msvideo"},
		{"语音.wav", "audio/wav"},
		{"没有扩展名", ""},
	}
	for _, c := range cases {
		k := media.Classify(c.fileName, c.contentType)
		fmt.Printf("  %-16s %-22s → %s\n", c.fileName, c.contentType, k)
	}

	// ── 二、容器归一化 ──────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 容器归一化 ===")
	for _, raw := range []string{"mov,mp4,m4a,3gp,3g2,mj2", "matroska,webm", "asf", "flv", "avi"} {
		fmt.Printf("  %-24q → %q\n", raw, media.NormalizeContainer(raw))
	}

	// ── 三、浏览器可播判定 ──────────────────────────────────────
	//
	// ⚠️ 空字段 = 判不出来 = **放行**。
	fmt.Println()
	fmt.Println("=== 浏览器可播 ===")
	playable := []*media.Info{
		{Kind: media.KindVideo, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac", Width: 1920, Height: 1080},
		{Kind: media.KindVideo, Container: "mkv", VideoCodec: "h264"},
		{Kind: media.KindVideo, Container: "avi", VideoCodec: "wmv3"},
		{Kind: media.KindVideo, Container: "webm", VideoCodec: "vp9"},
		{Kind: media.KindVideo}, // 全部字段空 ⇒ 判不出来
	}
	for _, info := range playable {
		reason := media.UnplayableReason(info)
		verdict := "可播"
		if reason != "" {
			verdict = "不可播:" + reason
		}
		fmt.Printf("  %-42s → %s\n", fmt.Sprintf("container=%s codec=%s", info.Container, info.VideoCodec), verdict)
	}

	// ── 四、播放文件扩展名 ──────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 播放扩展名 ===")
	fmt.Printf("  视频 → %q\n", media.PlaybackExt(media.KindVideo))
	fmt.Printf("  音频 → %q\n", media.PlaybackExt(media.KindAudio))

	// ── 五、转码参数 ────────────────────────────────────────────
	//
	// 参数是显式列出来的,不是拼字符串 —— 拼字符串意味着参数来自文件名时
	// 会有命令注入面。
	fmt.Println()
	fmt.Println("=== 转码参数 ===")
	for _, kind := range []media.Kind{media.KindVideo, media.KindAudio} {
		fmt.Printf("  %s:\n", kind)
		for _, a := range media.TranscodeArgs(kind, "in.xxx", "out.yyy") {
			fmt.Printf("    %s\n", a)
		}
	}

	// ── 六、大小提示 ────────────────────────────────────────────
	fmt.Println()
	fmt.Println("=== 大小提示 ===")
	for _, size := range []int64{5 << 20, 200 << 20, 2 << 30} {
		fmt.Printf("  %8d 字节 → %s\n", size, media.PlaybackSizeHint(size))
	}
}
