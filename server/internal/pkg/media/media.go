// 媒体探测 —— 用 ffprobe 读时长/分辨率,用 ffmpeg 抽封面帧(M141)
//
// # 这个包解决什么
//
// 文件列表的缩略图视图(M134)对图片能直接铺开,但**视频和音频只能摆一个图标**:
// 时长看不见、封面也看不见。用户 2026-10-31 提的:
// 「上传文件时候如果是视频文件,都调用环境变量里面默认的 ffmpeg 工具提取视频时长,
// 截取封面,这样 grid 里面就可以显示视频封面,同时显示视频时长;
// 如果上传的文件是音频格式的,封面就用音频图标,同时也要提取显示音频时长。」
//
// # 边界:这个包**只管跑命令、解析输出**
//
// 它不认识数据库、不认识存储、不认识用户 —— 那是 service 层的事。
// 分成两层的原因:命令怎么拼、输出怎么解析是**纯逻辑**,可以脱离 ffmpeg 测;
// 而"什么时候跑、结果存哪"是业务编排,测它就得把整个 service 搭起来。
//
// # ⚠️ **工具不可用 ≠ 报错**
//
// 开发机上没装 ffmpeg 是常态(ffmpeg 不是 Go 依赖,不会随仓库分发)。
// 所以"找不到二进制"必须是一个**可识别的哨兵错误**(ErrUnavailable),
// 由调用方决定降级 —— 上传照常成功,只是这个文件没有时长和封面。
// 把它当成"探测失败"泛化处理的话,日志里每次上传都会刷一条 error,
// 而用户看不出任何区别(前端就是少个时长)—— 那是噪音。
//
// # ⚠️ **命令执行器是注入的**
//
// Runner 接口让测试可以塞一个假执行器进去,断言**命令怎么拼、输出怎么解析**,
// 不需要真的装 ffmpeg。真实的 ffmpeg 集成测试是**另一件事**(见 media_integration_test.go,
// 装了 ffmpeg 才跑,没装就跳过 —— 跳过的那条会在测试名里写明原因)。
package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// Kind 媒体种类
type Kind string

const (
	// KindUnknown 不是音视频(或者判不出来)
	KindUnknown Kind = ""
	// KindVideo 视频
	KindVideo Kind = "video"
	// KindAudio 音频
	KindAudio Kind = "audio"
)

// ErrUnavailable 工具不可用(二进制找不到 / 不可执行)。
//
// ⚠️ **调用方必须能用 errors.Is 认出它** —— "没装 ffmpeg"和"文件解不开"
// 是两件完全不同的事:前者是部署状态,后者是文件本身有问题。
// 混在一起的话,运维看到满屏 error 会以为文件坏了,而实际上只是没装工具。
var ErrUnavailable = errors.New("media: ffmpeg/ffprobe 不可用")

// Info 探测结果
type Info struct {
	// Kind 媒体种类(video / audio)
	Kind Kind `json:"kind"`
	// DurationMs 时长(毫秒)。0 = 探测不到(比如容器没有时长信息)
	DurationMs int64 `json:"duration_ms"`
	// Width / Height 视频分辨率。音频是 0。
	Width  int `json:"width"`
	Height int `json:"height"`

	// Container 归一化后的容器名(mp4 / webm / mkv / avi / asf / flv …)。
	//
	// ⚠️ **空 = 判不出来** —— ffprobe 没吐 format_name 时就是空。
	//   按"不知道"处理(见 BrowserPlayable:某一项为空就不拦),
	//   **别**当成"浏览器一定不能播"。
	Container string `json:"container"`
	// VideoCodec 视频流编码名(h264 / hevc / vp9 / mpeg4 / wmv3 / flv1 …)。
	VideoCodec string `json:"video_codec"`
	// AudioCodec 音频流编码名(aac / mp3 / opus / wmav2 …)。没有音频流 = 空。
	AudioCodec string `json:"audio_codec"`
}

// Runner 跑外部命令。
//
// ⚠️ **返回 stdout + error,stderr 混在 error 里** —— ffprobe/ffmpeg 出错时
// 真正有价值的信息在 stderr("moov atom not found"之类),
// 只回一句 "exit status 1" 等于什么都没说。
type Runner interface {
	// Run 跑一条命令,返回 stdout。命令不存在时必须让错误能被
	// errors.Is(err, exec.ErrNotFound) 认出。
	Run(ctx context.Context, name string, args ...string) ([]byte, error)
}

// execRunner 默认实现:真的 exec
type execRunner struct{}

// Run 实现 Runner。
//
// ⚠️ **ctx 必须真的传下去** —— ffprobe 探一个坏文件可能卡住(网络盘上的
// 文件、大索引的 m3u8),没有 ctx 就是一个能把上传请求挂死的线程。
//
// ⚠️ **这里不判断"二进制不存在"** —— 那件事由 wrapRunErr 统一做。
// 放在这里的话,换个 Runner 实现(测试里的假执行器、将来的别的执行策略)
// 就得各自再实现一遍,漏一处就少一个 ErrUnavailable。
func (execRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	// ⚠️ **stderr 单独收** —— 见 Runner 接口的说明。
	var stderr strings.Builder
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("%s %s: %w(%s)", name, strings.Join(args, " "), err, msg)
		}
		return nil, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

// wrapRunErr 把"二进制不存在"翻译成 ErrUnavailable。
//
// ⚠️ **必须用 errors.Is 而不是类型断言** —— exec.CommandContext 包出来的
// 错误是 *exec.Error,而调用方拿到的是被 %w 包过若干层的;
// 只有 errors.Is 能穿透。
func wrapRunErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	return err
}

// DefaultRunner 默认命令执行器
var DefaultRunner Runner = execRunner{}

// Probe 用 ffprobe 读时长和分辨率。
//
// ⚠️ **参数是 `-print_format json`** 而不是 `-show_entries format=duration`
// —— 后者只吐一行数字,拿不到"这是视频还是音频"(音频文件没有视频流,
// 而**这个区别决定了要不要抽封面帧**:音频的封面是图标,不是图片)。
//
// ⚠️ **时长优先取 format.duration,兜底取视频流的 duration** ——
// 某些容器(特别是 mpeg-ts / 部分 mp4)format.duration 是 "N/A",
// 而流上带着真实时长。
func Probe(ctx context.Context, ffprobeBin, path string) (*Info, error) {
	return probeWith(ctx, DefaultRunner, ffprobeBin, path)
}

// probeWith 内部实现(可注入 Runner)
func probeWith(ctx context.Context, r Runner, ffprobeBin, path string) (*Info, error) {
	out, err := r.Run(ctx, ffprobeBin,
		"-v", "error",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		path,
	)
	if err != nil {
		return nil, wrapRunErr(err)
	}
	var raw ffprobeOutput
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("media: 解析 ffprobe 输出失败: %w", err)
	}
	info := &Info{}
	// 先看流:有没有视频流 ⇒ 是视频,只有音频流 ⇒ 是音频。
	var videoStream *ffprobeStream
	for i := range raw.Streams {
		s := raw.Streams[i]
		switch s.CodecType {
		case "video":
			if videoStream == nil {
				videoStream = &raw.Streams[i]
			}
		case "audio":
			if info.Kind == KindUnknown {
				info.Kind = KindAudio
			}
			// ⚠️ **只记第一条音频流** —— 多音轨文件(配音 + 原声)取哪条都行,
			//   这里要的是"这个文件的音频编码是什么",不是"放哪条"。
			if info.AudioCodec == "" {
				info.AudioCodec = s.CodecName
			}
		}
	}
	if videoStream != nil {
		info.Kind = KindVideo
		info.Width = videoStream.Width
		info.Height = videoStream.Height
		info.VideoCodec = videoStream.CodecName
	}
	// 容器名 —— 判"浏览器能不能播"要用(见 playback.go)
	info.Container = NormalizeContainer(raw.Format.FormatName)
	// 时长:format 优先,视频流兜底。
	info.DurationMs = parseDurationMs(raw.Format.Duration)
	if info.DurationMs == 0 && videoStream != nil {
		info.DurationMs = parseDurationMs(videoStream.Duration)
	}
	return info, nil
}

// ExtractFrame 抽一帧存成 jpg。
//
// ⚠️ **`-ss` 放在 `-i` 前面**(输入端 seek)= 快但可能不准 ——
// 它跳到最近的关键帧。对"抽一张封面"来说完全够用;
// 放在 `-i` 后面(输出端 seek)才准,代价是要解完整个文件。
//
// ⚠️ **缩到 640 宽** —— 封面只是列表里的一张缩略图,原分辨率(4K)纯属浪费
// 存储和带宽。而且 ffprobe 出来的宽高比在缩放后要保持,`-2` 表示
// "高度取偶数"(H.264 要求偶数边长)。
//
// ⚠️ **调用方负责 atMs 落点** —— 见 FramePosition。
func ExtractFrame(ctx context.Context, ffmpegBin, in, outPath string, atMs int64) error {
	return extractFrameWith(ctx, DefaultRunner, ffmpegBin, in, outPath, atMs)
}

// extractFrameWith 内部实现(可注入 Runner)
func extractFrameWith(ctx context.Context, r Runner, ffmpegBin, in, outPath string, atMs int64) error {
	at := float64(atMs) / 1000
	_, err := r.Run(ctx, ffmpegBin,
		"-nostdin",
		"-y",
		"-ss", strconv.FormatFloat(at, 'f', 3, 64),
		"-i", in,
		"-frames:v", "1",
		"-vf", "scale=640:-2",
		"-q:v", "3",
		outPath,
	)
	return wrapRunErr(err)
}

// FramePosition 抽帧的时间点(毫秒)。
//
// ⚠️ **不是"从头开始"** —— 很多视频开头是黑屏/片头,抽出来是一片黑,
// 用户看到的就是"这个视频没有封面"。
//
// ⚠️ **短片取中点** —— 3 秒以内的视频,1 秒处可能已经在片尾了。
//
// ⚠️ **长片取 10%,但不超过 3 秒** —— 10 秒的视频落在 1 秒,
// 10 分钟的视频落在 3 秒(而不是 1 分钟 —— 那多半还在片头)。
func FramePosition(durationMs int64) int64 {
	switch {
	case durationMs <= 0:
		return 0
	case durationMs < 3000:
		return durationMs / 2
	default:
		tenth := durationMs / 10
		if tenth > 3000 {
			return 3000
		}
		return tenth
	}
}

// Classify 按文件名/类型判是不是音视频。
//
// ⚠️ **扩展名当主判据** —— 和前端那套图标映射(EXT_ICONS)保持一致。
// MIME 那条路不可靠:Go 的 mime.TypeByExtension 在 Windows 上会去查注册表,
// `.mkv` / `.flac` 这种冷门格式**不一定**有值,查不到就落到
// `application/octet-stream` ⇒ 视频被判成"不是媒体" ⇒ 不抽封面。
//
// ⚠️ **MIME 只在扩展名认不出来时兜底** —— 比如没有扩展名的文件。
func Classify(fileName, contentType string) Kind {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	if ext != "" {
		if videoExts[ext] {
			return KindVideo
		}
		if audioExts[ext] {
			return KindAudio
		}
		// ⚠️ **扩展名认出来了但不在媒体名单里 ⇒ 就是不是媒体**,
		//   别再拿 content_type 兜底 —— 一个叫 `a.mp4.txt` 的文本文件
		//   会被浏览器报成 `text/plain`,而用户眼里它就是个 txt。
		return KindUnknown
	}
	ct := strings.ToLower(strings.TrimSpace(contentType))
	switch {
	case strings.HasPrefix(ct, "video/"):
		return KindVideo
	case strings.HasPrefix(ct, "audio/"):
		return KindAudio
	}
	return KindUnknown
}

// videoExts 视频扩展名
//
// ⚠️ **判据是"能不能被 ffmpeg 解"** —— 不是"官方标准格式"。
// mkv / flv / wmv 这些在 Windows 上双击打不开,但 ffmpeg 全都能解,
// 而"用户传了个视频结果列表里只有个图标"正是这个功能要解决的。
var videoExts = map[string]bool{
	"mp4": true, "m4v": true, "mov": true, "mkv": true, "webm": true,
	"avi": true, "flv": true, "wmv": true, "mpg": true, "mpeg": true,
	"m2ts": true, "ts": true, "mts": true, "m2v": true, "3gp": true,
	"ogv": true, "rmvb": true, "rm": true, "asf": true, "vob": true,
}

// audioExts 音频扩展名
var audioExts = map[string]bool{
	"mp3": true, "wav": true, "flac": true, "aac": true, "m4a": true,
	"ogg": true, "oga": true, "opus": true, "wma": true, "amr": true,
	"aiff": true, "aif": true, "ape": true, "mid": true, "midi": true,
}

// ffprobeOutput ffprobe 的 JSON 输出
type ffprobeOutput struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

type ffprobeStream struct {
	CodecType string `json:"codec_type"`
	// CodecName 编码名(h264 / aac / mpeg4 …)—— 判"浏览器能不能播"要用
	CodecName string `json:"codec_name"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
	// Duration 流级时长,某些容器只有它有值(字符串,可能是 "N/A")
	Duration string `json:"duration"`
}

type ffprobeFormat struct {
	// Duration 容器时长(秒,字符串,可能是 "N/A")
	Duration string `json:"duration"`
	// FormatName 容器名,逗号分隔,越靠前越泛
	// (mp4 是 "mov,mp4,m4a,3gp,3g2,mj2",webm 是 "matroska,webm")
	FormatName string `json:"format_name"`
}

// parseDurationMs 解析 ffprobe 的时长字符串(秒 → 毫秒)。
//
// ⚠️ **"N/A" 必须当成"没有时长"** —— ffprobe 对拿不到时长的容器会写字符串
// "N/A",`strconv.ParseFloat` 会报错。报错的话整次探测就失败了,
// 而实际只是"这个文件没有时长信息"(比如某些流媒体切片)。
//
// ⚠️ **负数当成 0** —— 有容器会报出负时长,那是坏数据,不是"负时长"。
func parseDurationMs(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "N/A" {
		return 0
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f <= 0 {
		return 0
	}
	return int64(f * 1000)
}
