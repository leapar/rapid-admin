// 浏览器可播判据 + 转码参数的单元测试(M145)
//
// ⚠️ **判据表是最容易写错的地方** —— 写宽了(把 hevc 算成可播)前端会显示
// "可播",用户点开还是播不了;写窄了(把 h264 算成不可播)会白白转一遍码,
// 白烧 CPU 和存储。
//
// ⇒ 所以这里**逐条**把边界编码列出来断言,而不是只测两个"典型"用例。
package media

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNormalizeContainer 容器归一化。
//
// ⚠️ **"matroska,webm" 必须判成 webm 而不是 mkv** ——
// webm 里面装的就是 matroska,判错容器会把"浏览器能播的 webm"判成不可播,
// 白转一遍码。
func TestNormalizeContainer(t *testing.T) {
	cases := []struct {
		in   string
		want string
		why  string
	}{
		{"mov,mp4,m4a,3gp,3g2,mj2", "mp4", "mp4 的 format_name 是一长串,第一项是 mov"},
		{"matroska,webm", "webm", "webm 也在里面 ⇒ 是 webm 不是 mkv"},
		{"matroska", "mkv", "纯 mkv 只有 matroska"},
		{"avi", "avi", "avi 原样"},
		{"asf", "asf", "wmv 是 asf 容器"},
		{"flv", "flv", "flv 原样"},
		{"mpegts", "mpegts", "ts 直播切片"},
		{"wav", "wav", "wav 原样"},
		{"mp3", "mp3", "mp3 原样"},
		{"ogg", "ogg", "ogg 原样"},
		{"flac", "flac", "flac 原样"},
		{"", "", "空 = 判不出来"},
		{"WEBM,MATROSKA", "webm", "大小写不敏感"},
	}
	for _, c := range cases {
		assert.Equal(t, c.want, NormalizeContainer(c.in), c.why)
	}
}

// TestBrowserPlayable_Video 视频能不能播的判据。
func TestBrowserPlayable_Video(t *testing.T) {
	cases := []struct {
		name      string
		info      *Info
		playable  bool
		reasonHas string
	}{
		{
			name:     "mp4 + h264 + aac = 能播",
			info:     &Info{Kind: KindVideo, Container: "mp4", VideoCodec: "h264", AudioCodec: "aac"},
			playable: true,
		},
		{
			name:     "mp4 + h264 无音轨 = 能播",
			info:     &Info{Kind: KindVideo, Container: "mp4", VideoCodec: "h264"},
			playable: true,
		},
		{
			name:     "webm + vp9 + opus = 能播",
			info:     &Info{Kind: KindVideo, Container: "webm", VideoCodec: "vp9", AudioCodec: "opus"},
			playable: true,
		},
		{
			name:      "mkv 容器 = 不能播(浏览器不解 mkv,装什么编码都没用)",
			info:      &Info{Kind: KindVideo, Container: "mkv", VideoCodec: "h264", AudioCodec: "aac"},
			playable:  false,
			reasonHas: "MKV",
		},
		{
			name:      "avi 容器 = 不能播",
			info:      &Info{Kind: KindVideo, Container: "avi", VideoCodec: "mpeg4"},
			playable:  false,
			reasonHas: "AVI",
		},
		{
			name:      "mpeg4 part 2 = 不能播(传上去播不了的头号来源)",
			info:      &Info{Kind: KindVideo, Container: "mp4", VideoCodec: "mpeg4", AudioCodec: "aac"},
			playable:  false,
			reasonHas: "MPEG-4 Part 2",
		},
		{
			name:      "hevc = 不能播(Safari 行,Chrome/Firefox 依平台)",
			info:      &Info{Kind: KindVideo, Container: "mp4", VideoCodec: "hevc", AudioCodec: "aac"},
			playable:  false,
			reasonHas: "H.265",
		},
		{
			name:      "wmv = 不能播",
			info:      &Info{Kind: KindVideo, Container: "asf", VideoCodec: "wmv3", AudioCodec: "wmav2"},
			playable:  false,
			reasonHas: "WMV",
		},
		{
			name:      "flv = 不能播",
			info:      &Info{Kind: KindVideo, Container: "flv", VideoCodec: "flv1", AudioCodec: "mp3"},
			playable:  false,
			reasonHas: "FLV",
		},
		{
			name:      "mp4 容器里的 ac3 音轨 = 不能播(画面能看,声音要装 Dolby 插件)",
			info:      &Info{Kind: KindVideo, Container: "mp4", VideoCodec: "h264", AudioCodec: "ac3"},
			playable:  false,
			reasonHas: "AC-3",
		},
		{
			name:     "判不出来 = 假定能播(别白白转一遍码)",
			info:     &Info{Kind: KindVideo},
			playable: true,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.playable, BrowserPlayable(c.info))
			reason := UnplayableReason(c.info)
			if c.reasonHas == "" {
				assert.Empty(t, reason)
			} else {
				assert.Contains(t, reason, c.reasonHas)
			}
		})
	}
}

// TestBrowserPlayable_Audio 纯音频的判据。
func TestBrowserPlayable_Audio(t *testing.T) {
	cases := []struct {
		name     string
		info     *Info
		playable bool
	}{
		{"mp3", &Info{Kind: KindAudio, Container: "mp3", AudioCodec: "mp3"}, true},
		{"m4a + aac", &Info{Kind: KindAudio, Container: "m4a", AudioCodec: "aac"}, true},
		{"wav + pcm", &Info{Kind: KindAudio, Container: "wav", AudioCodec: "pcm_s16le"}, true},
		{"flac", &Info{Kind: KindAudio, Container: "flac", AudioCodec: "flac"}, true},
		{"ogg + vorbis(Safari 不解 ogg)", &Info{Kind: KindAudio, Container: "ogg", AudioCodec: "vorbis"}, false},
		{"wma", &Info{Kind: KindAudio, Container: "asf", AudioCodec: "wmav2"}, false},
		{"amr", &Info{Kind: KindAudio, Container: "amr", AudioCodec: "amr_nb"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.playable, BrowserPlayable(c.info))
		})
	}
}

// TestBrowserPlayable_Unknown 非媒体不转码。
func TestBrowserPlayable_Unknown(t *testing.T) {
	assert.True(t, BrowserPlayable(nil))
	assert.True(t, BrowserPlayable(&Info{Kind: KindUnknown}))
	assert.Empty(t, UnplayableReason(nil))
}

// TestTranscodeArgs_Video 视频转码参数。
//
// ⚠️ **这些断言每一条都在防一个具体的坑**:
//   - `-map` 少了 ⇒ 字幕轨被搬进 mp4 ⇒ mux 失败
//   - `-map 0:a:0?` 的 `?` 少了 ⇒ 没音轨的视频转码失败
//   - `-movflags +faststart` 少了 ⇒ 浏览器必须下完整个文件才起播
//   - `-pix_fmt yuv420p` 少了 ⇒ 部分设备解不了
func TestTranscodeArgs_Video(t *testing.T) {
	args := TranscodeArgs(KindVideo, "/tmp/in.mkv", "/tmp/out.mp4")
	s := strings.Join(args, " ")

	assert.True(t, strings.HasSuffix(s, "/tmp/out.mp4"), "输出路径必须在最后")
	assert.Contains(t, s, "-i /tmp/in.mkv")
	assert.Contains(t, s, "-map 0:v:0")
	assert.Contains(t, s, "-map 0:a:0?")
	assert.Contains(t, s, "-c:v libx264")
	assert.Contains(t, s, "-c:a aac")
	assert.Contains(t, s, "-movflags +faststart")
	assert.Contains(t, s, "-pix_fmt yuv420p")
	assert.Contains(t, s, "-sn -dn")
	assert.NotContains(t, s, "-vn", "视频不能加 -vn(会把视频流丢掉)")
}

// TestTranscodeArgs_Audio 音频转码参数。
func TestTranscodeArgs_Audio(t *testing.T) {
	args := TranscodeArgs(KindAudio, "/tmp/in.wma", "/tmp/out.m4a")
	s := strings.Join(args, " ")

	assert.Contains(t, s, "-vn", "纯音频必须加 -vn")
	assert.NotContains(t, s, "-c:v", "纯音频不该给视频编码器(ffmpeg 会报'没有视频流')")
	assert.Contains(t, s, "-c:a aac")
	assert.Contains(t, s, "-map 0:a:0")
	assert.True(t, strings.HasSuffix(s, "/tmp/out.m4a"))
}

// TestTranscode_UsesRunner 转码真的调了 ffmpeg。
func TestTranscode_UsesRunner(t *testing.T) {
	r := &fakeRunner{}
	err := transcodeWith(context.Background(), r, "ffmpeg", "/tmp/in.mkv", "/tmp/out.mp4", KindVideo)
	require.NoError(t, err)
	assert.Equal(t, "ffmpeg", r.gotName)
	assert.Contains(t, r.argsString(), "-c:v libx264")
}

// TestTranscode_ReportsMissingBinary 没装 ffmpeg 时报 ErrUnavailable。
//
// ⚠️ **必须能 errors.Is 认出它** —— 调用方靠它决定"跳过转码"还是"转码失败"。
func TestTranscode_ReportsMissingBinary(t *testing.T) {
	r := &fakeRunner{err: exec.ErrNotFound}
	err := transcodeWith(context.Background(), r, "ffmpeg", "/tmp/in.mkv", "/tmp/out.mp4", KindVideo)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnavailable), "err = %v,应该能被 errors.Is 认成 ErrUnavailable", err)
}

// TestTranscode_PropagatesRealError 真失败要原样传出去。
func TestTranscode_PropagatesRealError(t *testing.T) {
	boom := errors.New("转码炸了")
	r := &fakeRunner{err: boom}
	err := transcodeWith(context.Background(), r, "ffmpeg", "/tmp/in.mkv", "/tmp/out.mp4", KindVideo)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "转码炸了")
}

// TestPlaybackExt 后缀决定 MIME。
func TestPlaybackExt(t *testing.T) {
	assert.Equal(t, ".mp4", PlaybackExt(KindVideo))
	assert.Equal(t, ".m4a", PlaybackExt(KindAudio))
}

// TestProbe_FillsCodecFields ffprobe 的编码信息要被解析出来。
//
// ⚠️ **这条是判"浏览器能不能播"的数据来源** —— 解析不出来就永远判成"能播",
// 于是 wmv/mkv 那些文件永远不会被转码。
func TestProbe_FillsCodecFields(t *testing.T) {
	const raw = `{
	  "streams": [
	    {"codec_type": "video", "codec_name": "h264", "width": 1280, "height": 720},
	    {"codec_type": "audio", "codec_name": "aac"}
	  ],
	  "format": {"duration": "8.000000", "format_name": "mov,mp4,m4a,3gp,3g2,mj2"}
	}`
	r := &fakeRunner{stdout: raw}
	info, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, "mp4", info.Container)
	assert.Equal(t, "h264", info.VideoCodec)
	assert.Equal(t, "aac", info.AudioCodec)
	assert.True(t, BrowserPlayable(info))
}

// TestProbe_NoAudioStream 没音轨时 AudioCodec 是空串(不是 "unknown")。
func TestProbe_NoAudioStream(t *testing.T) {
	const raw = `{
	  "streams": [
	    {"codec_type": "video", "codec_name": "h264", "width": 640, "height": 480}
	  ],
	  "format": {"duration": "3.000000", "format_name": "matroska"}
	}`
	r := &fakeRunner{stdout: raw}
	info, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.mkv")
	require.NoError(t, err)
	assert.Equal(t, "mkv", info.Container)
	assert.Empty(t, info.AudioCodec)
	assert.False(t, BrowserPlayable(info), "mkv 容器就是不可播")
}
