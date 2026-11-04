// 浏览器能不能播 + 转码 —— M145
//
// # 这个文件解决什么
//
// M141 抽封面用的是 ffmpeg,而 **ffmpeg 能解 ≠ 浏览器能播**。
// WMV / FLV / MKV / MPEG-4 Part 2 这些格式 ffmpeg 全都能解,
// 于是时长能探出来、封面也能抽出来 —— **看起来一切正常**,
// 但用户点开那个直链,浏览器下载它而不是播它。
//
// 「看起来正常但其实播不了」是最坏的一种状态:没有报错、没有提示,
// 用户只会觉得"这平台上传视频有问题"。
//
// ⇒ 所以探测的时候要**顺带判"浏览器能不能播"**,不能播的转一份 H.264/AAC。
//
// # ⚠️ **判据是"全平台通吃",不是"某个浏览器能播"**
//
// Safari 不支持 Ogg/Vorbis,Firefox 不支持 HEVC,Chrome 对 HEVC 依平台 ——
// 只要"有一个主流浏览器播不了",这个文件就不能算"浏览器可播"。
// 用户不会为了看一个视频去换浏览器。
//
// # ⚠️ **容器和编码是两件事,都要查**
//
// MKV 里面装 H.264 —— 编码是支持的,但**浏览器不解 MKV 容器** ⇒ 还是播不了。
// 同理 MP4 容器里装 AC-3 音频 ⇒ 视频能看,声音要装 Dolby 插件。
package media

import (
	"context"
	"strconv"
	"strings"
)

// NormalizeContainer 把 ffprobe 的 format_name 归一化成容器名。
//
// ⚠️ **format_name 是逗号分隔的一串,越靠前越泛** ——
// mp4 是 "mov,mp4,m4a,3gp,3g2,mj2",webm 是 "matroska,webm",
// 而**纯 mkv 只有 "matroska"**。所以判 webm 必须看"webm 在不在里面",
// 不能只看第一项(第一项对两者都是 matroska)。
//
// ⚠️ **顺序很重要**:webm 必须排在 matroska 前面判 ——
// "matroska,webm" 里含 webm ⇒ 是 webm;只含 matroska ⇒ 是 mkv。
func NormalizeContainer(formatName string) string {
	f := strings.ToLower(strings.TrimSpace(formatName))
	if f == "" {
		return ""
	}
	has := func(k string) bool { return strings.Contains(","+f+",", ","+k+",") }
	switch {
	case has("webm"):
		return "webm"
	case has("mp4"), has("mov"), has("m4a"), has("m4v"):
		return "mp4"
	case has("matroska"):
		return "mkv"
	case has("avi"):
		return "avi"
	case has("asf"):
		return "asf"
	case has("flv"):
		return "flv"
	case has("mpegts"), has("mpeg-ts"):
		return "mpegts"
	case has("wav"):
		return "wav"
	case has("mp3"):
		return "mp3"
	case has("ogg"):
		return "ogg"
	case has("flac"):
		return "flac"
	case has("aac"):
		return "aac"
	case has("mpegvideo"), has("mpeg"):
		return "mpeg"
	case has("rtsp"), has("hls"):
		return "streaming"
	}
	// 认不出来就返回第一项 —— 至少还有个名字能显示给人看
	first := strings.Split(f, ",")[0]
	return strings.TrimSpace(first)
}

// playableVideoContainers 浏览器能播的容器(视频)
//
// ⚠️ **没有 mkv** —— 浏览器根本不解这个容器,里面装什么编码都没用。
// ⚠️ **没有 ogg** —— Safari 不支持(视频方面)。
// ⚠️ **mov 归到 mp4** —— 同一个 demuxer,浏览器按 mp4 嗅探,能播。
var playableVideoContainers = map[string]bool{
	"mp4":  true,
	"webm": true,
}

// playableVideoCodecs 浏览器能播的视频编码
//
// ⚠️ **没有 hevc** —— Safari 能播,Chrome/Firefox 依平台(硬解/软解/系统组件),
// 为了"所有浏览器都能播"统一当不可播。
// ⚠️ **没有 mpeg4** —— 那是 MPEG-4 Part 2(DivX/Xvid),**浏览器完全不支持**,
// 但它是"传上去播不了"的头号来源(老录像、老课件、DV 转出来的)。
var playableVideoCodecs = map[string]bool{
	"h264": true,
	"vp8":  true,
	"vp9":  true,
	"av1":  true,
}

// playableAudioCodecs 浏览器能播的音频编码(视频里的音轨、纯音频都按这张表)
var playableAudioCodecs = map[string]bool{
	"aac":  true,
	"mp3":  true,
	"opus": true,
	"flac": true,
	// ⚠️ vorbis 只在 webm 容器里算数(ogg 容器 Safari 不支持,整体已被排除)
	"vorbis": true,
	// WAV 里的 PCM —— 浏览器原生支持
	"pcm_s16le": true,
	"pcm_s24le": true,
	"pcm_u8":    true,
	"pcm_f32le": true,
	"pcm_s32le": true,
}

// playableAudioContainers 浏览器能播的容器(纯音频)
var playableAudioContainers = map[string]bool{
	"mp3":  true,
	"m4a":  true,
	"mp4":  true,
	"wav":  true,
	"flac": true,
	"webm": true,
	"opus": true,
}

// BrowserPlayable 判断浏览器能不能**直接**播这个文件。
//
// ⚠️ **每一项都是"已知且不好"才拦下来** ——
// 某一项是空(判不出来)就**不拦**。理由:转码要烧 CPU、占存储,
// "拿不准"不该触发它。探测失败的文件保持原样,用户至少还能下载。
//
// ⚠️ 但"已知且不好"照样拦 —— 容器读不出来、但编码明确是 mpeg4,
// 那还是得转(编码这一条已经够判死刑了)。
//
// ⚠️ **视频的音轨也要查** —— 一个 MP4 容器里装 AC-3 音频,
// 画面能看但声音要装 Dolby 插件 ⇒ 整段视频在大部分机器上"没声音"。
func BrowserPlayable(info *Info) bool {
	return UnplayableReason(info) == ""
}

// UnplayableReason 给出"为什么浏览器播不了"的一句话(给前端显示)。
//
// ⚠️ **空字符串 = 能播** —— 调用方拿它当"要不要转码"的另一个判据。
// ⚠️ **判据和 BrowserPlayable 是同一个** —— 两个函数各判一次的话,
// 迟早会漂移(一个说能播另一个说不能),而那种不一致极难查。
func UnplayableReason(info *Info) string {
	if info == nil || info.Kind == KindUnknown {
		return ""
	}
	switch info.Kind {
	case KindVideo:
		if info.Container != "" && !playableVideoContainers[info.Container] {
			return "容器 " + displayContainer(info.Container) + " 浏览器不支持"
		}
		if info.VideoCodec != "" && !playableVideoCodecs[info.VideoCodec] {
			return "视频编码 " + displayCodec(info.VideoCodec) + " 浏览器不支持"
		}
		if info.AudioCodec != "" && !playableAudioCodecs[info.AudioCodec] {
			return "音频编码 " + displayCodec(info.AudioCodec) + " 浏览器不支持"
		}
	case KindAudio:
		if info.Container != "" && !playableAudioContainers[info.Container] {
			return "容器 " + displayContainer(info.Container) + " 浏览器不支持"
		}
		if info.AudioCodec != "" && !playableAudioCodecs[info.AudioCodec] {
			return "音频编码 " + displayCodec(info.AudioCodec) + " 浏览器不支持"
		}
	}
	return ""
}

// displayContainer 容器的展示名
func displayContainer(c string) string {
	switch c {
	case "":
		return "未知"
	case "mp4":
		return "MP4"
	case "webm":
		return "WebM"
	case "mkv":
		return "MKV"
	case "avi":
		return "AVI"
	case "asf":
		return "WMV/ASF"
	case "flv":
		return "FLV"
	case "mpegts":
		return "MPEG-TS"
	case "wav":
		return "WAV"
	case "mp3":
		return "MP3"
	case "ogg":
		return "Ogg"
	case "flac":
		return "FLAC"
	case "aac":
		return "AAC"
	case "mpeg":
		return "MPEG"
	case "streaming":
		return "流媒体"
	}
	return strings.ToUpper(c)
}

// displayCodec 编码的展示名
func displayCodec(c string) string {
	switch c {
	case "":
		return "未知"
	case "h264":
		return "H.264"
	case "hevc":
		return "H.265"
	case "vp8":
		return "VP8"
	case "vp9":
		return "VP9"
	case "av1":
		return "AV1"
	case "mpeg4":
		return "MPEG-4 Part 2"
	case "aac":
		return "AAC"
	case "mp3":
		return "MP3"
	case "opus":
		return "Opus"
	case "vorbis":
		return "Vorbis"
	case "flac":
		return "FLAC"
	case "wmav2":
		return "WMA"
	case "wmv3":
		return "WMV"
	case "flv1":
		return "FLV"
	case "ac3":
		return "AC-3"
	case "eac3":
		return "E-AC-3"
	case "amr_nb":
		return "AMR"
	case "pcm_s16le":
		return "PCM"
	}
	return strings.ToUpper(c)
}

// TranscodeArgs 拼转码命令的参数(纯函数,可测)。
//
// ⚠️ **目标是 H.264 + AAC / MP4** —— 这是"所有浏览器都能播"的那一档:
// MP4 容器 + H.264 视频 + AAC 音频。
//
// ⚠️ **`-map` 必须显式给** —— 不给的话 ffmpeg 会把**所有流**都搬过去,
// 包括字幕轨和数据轨。WMV/MKV 里的字幕编码(ass/srt/dvd_subtitle)mp4 装不下,
// 表现是"转码到一半失败",而错误信息是"Could not find tag for codec … in stream #1,
// codec not currently supported in container" —— 极难一眼看懂。
// `-map 0:a:0?` 末尾那个 `?` = 音频轨可选(有些视频没音轨)。
//
// ⚠️ **`-sn -dn` 丢字幕和数据轨** —— 双保险。
//
// ⚠️ **`-movflags +faststart`** —— moov atom 放文件头。
// 不加的话 moov 在文件尾,浏览器**必须把整个文件下完**才起播
// (本地文件无所谓,OSS 上的直链就是"点了没反应,转圈半天")。
//
// ⚠️ **`-pix_fmt yuv420p`** —— H.264 的 yuv444p 在很多设备上解不了;
// 浏览器软解也未必支持。420 是兼容性最广的那一档。
//
// ⚠️ **`-preset veryfast`** —— 转码质量和体积的取舍。给用户看的视频不需要
// 慢悠悠地压;`veryfast` 大约是 `medium` 的两倍速度,体积只大一点。
//
// ⚠️ **音频不迁视频流**(`-vn`) —— 纯音频文件转码时如果带 `-c:v` 会报错
// ("只有音频流,没有视频流")。
func TranscodeArgs(kind Kind, in, out string) []string {
	args := []string{"-nostdin", "-y", "-i", in}
	if kind == KindVideo {
		args = append(args,
			"-map", "0:v:0",
			"-map", "0:a:0?",
			"-c:v", "libx264",
			"-preset", "veryfast",
			"-crf", "23",
			"-pix_fmt", "yuv420p",
			"-c:a", "aac",
			"-b:a", "128k",
			"-movflags", "+faststart",
		)
	} else {
		args = append(args,
			"-map", "0:a:0",
			"-vn",
			"-c:a", "aac",
			"-b:a", "128k",
		)
	}
	args = append(args, "-sn", "-dn", out)
	return args
}

// Transcode 转码成一个浏览器能播的副本。
func Transcode(ctx context.Context, ffmpegBin, in, out string, kind Kind) error {
	return transcodeWith(ctx, DefaultRunner, ffmpegBin, in, out, kind)
}

// transcodeWith 内部实现(可注入 Runner)
func transcodeWith(ctx context.Context, r Runner, ffmpegBin, in, out string, kind Kind) error {
	_, err := r.Run(ctx, ffmpegBin, TranscodeArgs(kind, in, out)...)
	return wrapRunErr(err)
}

// PlaybackExt 播放副本的扩展名。
//
// ⚠️ **视频是 .mp4,音频是 .m4a** —— 两者容器都是 MP4 系,
// 但后缀不同:浏览器看后缀决定用哪个 MIME(`.m4a` → audio/mp4,
// `.mp4` → video/mp4)。后缀给错,浏览器会拿 video 播放器去放纯音频
// (或者反过来)。
func PlaybackExt(kind Kind) string {
	if kind == KindVideo {
		return ".mp4"
	}
	return ".m4a"
}

// PlaybackSizeHint 转码产物的体积说明(给日志用)。
//
// ⚠️ **H.264 CRF 23 的产物通常比原文件小**(尤其是 WMV/FLV/MPEG-4 Part 2
// 这些"编码效率低"的格式),但**低码率的 DivX 反过来会变大** ——
// 所以不能假设"转码完一定更小"。
func PlaybackSizeHint(originalSize int64) string {
	if originalSize <= 0 {
		return "原文件大小未知"
	}
	return "原文件 " + strconv.FormatInt(originalSize/1024/1024, 10) + " MB"
}
