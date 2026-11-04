// 媒体探测的单元测试(M141)
//
// ⚠️ **这里不跑真的 ffmpeg** —— 命令执行器是注入的(见 fakeRunner),
// 所以这些用例在**任何机器上都能跑**,包括没装 ffmpeg 的开发机。
// 真的 ffmpeg 集成测试是另一条线(media_integration_test.go)。
//
// ⚠️ **断言的是"命令怎么拼 + 输出怎么解析"** —— 这两处才是真会写错的地方。
// "ffmpeg 装上了能不能跑通"是部署问题,不是这个包的逻辑问题。
package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeRunner 假命令执行器:记录命令,返回预设输出。
type fakeRunner struct {
	// stdout 每次都返回这段
	stdout string
	// err 返回的错误
	err error
	// 记录下来的命令
	gotName string
	gotArgs []string
	// 写文件:执行时把内容写到这个路径(模拟 ffmpeg 输出 jpg)
	writeTo string
}

func (f *fakeRunner) Run(ctx context.Context, name string, args ...string) ([]byte, error) {
	f.gotName = name
	f.gotArgs = args
	if f.err != nil {
		return nil, f.err
	}
	if f.writeTo != "" {
		// 模拟 ffmpeg 真的落了一个文件
		_ = os.WriteFile(f.writeTo, []byte("fake-jpeg"), 0o644)
	}
	return []byte(f.stdout), nil
}

// argsString 把参数拼成一行,便于断言
func (f *fakeRunner) argsString() string {
	return strings.Join(f.gotArgs, " ")
}

const videoProbeJSON = `{
  "streams": [
    {"codec_type": "video", "width": 1920, "height": 1080, "duration": "12.500000"},
    {"codec_type": "audio", "duration": "12.500000"}
  ],
  "format": {"duration": "12.500000"}
}`

// TestProbe_ParsesVideoInfo 特征:
// 视频文件探测出**种类 + 时长 + 分辨率**三样。
func TestProbe_ParsesVideoInfo(t *testing.T) {
	r := &fakeRunner{stdout: videoProbeJSON}
	info, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, KindVideo, info.Kind)
	assert.Equal(t, int64(12500), info.DurationMs)
	assert.Equal(t, 1920, info.Width)
	assert.Equal(t, 1080, info.Height)

	// ⚠️ **命令形状也要断言** —— 参数顺序错了 ffprobe 会直接报错,
	// 而报错信息看不出是哪个参数错了。
	assert.Equal(t, "ffprobe", r.gotName)
	assert.Contains(t, r.argsString(), "-print_format json")
	assert.Contains(t, r.argsString(), "-show_format")
	assert.Contains(t, r.argsString(), "-show_streams")
	assert.True(t, strings.HasSuffix(r.argsString(), "/tmp/a.mp4"))
}

// TestProbe_AudioOnlyHasNoVideoStream 特征:
// **纯音频文件探测出 Kind=audio,分辨率为 0** ——
// 音频的"封面"是图标不是图片,靠的就是这个区分。
func TestProbe_AudioOnlyHasNoVideoStream(t *testing.T) {
	r := &fakeRunner{stdout: `{
      "streams": [{"codec_type": "audio", "duration": "61.000000"}],
      "format": {"duration": "61.000000"}
    }`}
	info, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.mp3")
	require.NoError(t, err)
	assert.Equal(t, KindAudio, info.Kind)
	assert.Equal(t, int64(61000), info.DurationMs)
	assert.Equal(t, 0, info.Width)
	assert.Equal(t, 0, info.Height)
}

// TestProbe_DurationFallsBackToStream 特征:
// **format.duration 是 "N/A" 时,退到流上的时长** ——
// 某些容器(尤其 mpeg-ts)容器层没有时长,只有流上有。
func TestProbe_DurationFallsBackToStream(t *testing.T) {
	r := &fakeRunner{stdout: `{
      "streams": [{"codec_type": "video", "width": 640, "height": 480, "duration": "8.250000"}],
      "format": {"duration": "N/A"}
    }`}
	info, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.ts")
	require.NoError(t, err)
	assert.Equal(t, int64(8250), info.DurationMs)
	assert.Equal(t, KindVideo, info.Kind)
}

// TestProbe_NoDurationIsZeroNotError 特征:
// **完全拿不到时长 ⇒ 0,不是报错** ——
// 探测不出时长不该让整个上传失败(前端就是少显示一个时长)。
func TestProbe_NoDurationIsZeroNotError(t *testing.T) {
	r := &fakeRunner{stdout: `{
      "streams": [{"codec_type": "video", "width": 320, "height": 240}],
      "format": {"duration": "N/A"}
    }`}
	info, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.mp4")
	require.NoError(t, err)
	assert.Equal(t, int64(0), info.DurationMs)
}

// TestProbe_BinaryMissingIsUnavailable 特征:
// **找不到二进制 ⇒ ErrUnavailable**,而且能被 errors.Is 认出。
//
// ⚠️ 这一条是"没装 ffmpeg"和"文件坏了"的分界线 —— 调用方靠它决定
// 是安静降级还是报错。
func TestProbe_BinaryMissingIsUnavailable(t *testing.T) {
	r := &fakeRunner{err: &exec.Error{Name: "ffprobe", Err: exec.ErrNotFound}}
	_, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.mp4")
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnavailable), "必须是 ErrUnavailable,实际:%v", err)
}

// TestProbe_NonJSONOutputFails 特征:
// ffprobe 吐了不是 JSON ⇒ 报错(而不是当成"没有时长"蒙混过关)。
func TestProbe_NonJSONOutputFails(t *testing.T) {
	r := &fakeRunner{stdout: "not json at all"}
	_, err := probeWith(context.Background(), r, "ffprobe", "/tmp/a.mp4")
	require.Error(t, err)
	assert.False(t, errors.Is(err, ErrUnavailable))
}

// TestExtractFrame_CommandShape 特征:
// 抽帧命令的形状 —— **-ss 在 -i 前面**(快 seek)、只取一帧、缩到 640 宽。
func TestExtractFrame_CommandShape(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cover.jpg")
	r := &fakeRunner{writeTo: out}
	err := extractFrameWith(context.Background(), r, "ffmpeg", "/tmp/a.mp4", out, 1500)
	require.NoError(t, err)

	assert.Equal(t, "ffmpeg", r.gotName)
	args := r.gotArgs
	// -ss 必须在 -i 前面
	ssIdx, iIdx := indexOf(args, "-ss"), indexOf(args, "-i")
	require.NotEqual(t, -1, ssIdx)
	require.NotEqual(t, -1, iIdx)
	assert.Less(t, ssIdx, iIdx, "-ss 必须在 -i 前面(输入端快 seek)")
	// -ss 的值 = 毫秒转秒
	assert.Equal(t, "1.500", args[ssIdx+1])
	// 只取一帧
	assert.Contains(t, r.argsString(), "-frames:v 1")
	// 缩放到 640 宽
	assert.Contains(t, r.argsString(), "scale=640:-2")
	// 输出路径在最后
	assert.Equal(t, out, args[len(args)-1])
}

// TestExtractFrame_BinaryMissingIsUnavailable 特征:
// 同上,抽帧也要能认出"工具没装"。
func TestExtractFrame_BinaryMissingIsUnavailable(t *testing.T) {
	out := filepath.Join(t.TempDir(), "cover.jpg")
	r := &fakeRunner{err: &exec.Error{Name: "ffmpeg", Err: exec.ErrNotFound}}
	err := extractFrameWith(context.Background(), r, "ffmpeg", "/tmp/a.mp4", out, 1000)
	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrUnavailable), "必须是 ErrUnavailable,实际:%v", err)
}

// TestFramePosition 特征:抽帧落点。
//
// ⚠️ **短片取中点、其余取 10% 且不超过 3 秒** ——
// 开头多半是黑屏或片头,抽出来一片黑就等于"这个视频没有封面"。
func TestFramePosition(t *testing.T) {
	cases := []struct {
		name string
		dur  int64
		want int64
	}{
		{"不知道时长就抽第一帧", 0, 0},
		{"1 秒的片子取一半", 1000, 500},
		{"2.9 秒取一半", 2900, 1450},
		{"10 秒取 10%", 10000, 1000},
		{"30 秒取 10%", 30000, 3000},
		{"10 分钟封顶 3 秒", 600000, 3000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, FramePosition(c.dur))
		})
	}
}

// TestClassify 特征:按扩展名判音视频。
//
// ⚠️ **认得出扩展名但不在媒体名单里 ⇒ 直接不是媒体** ——
// 不拿 content_type 兜底,否则 `a.mp4.txt` 这种文件会被当成视频,
// 然后 ffmpeg 去解一个文本文件,白跑一趟还报错。
func TestClassify(t *testing.T) {
	cases := []struct {
		name     string
		fileName string
		ct       string
		want     Kind
	}{
		{"mp4 是视频", "a.mp4", "", KindVideo},
		{"大写扩展名也认", "A.MP4", "", KindVideo},
		{"mkv 是视频", "a.mkv", "", KindVideo},
		{"mov 是视频", "a.mov", "video/quicktime", KindVideo},
		{"mp3 是音频", "a.mp3", "", KindAudio},
		{"flac 是音频", "a.flac", "", KindAudio},
		{"pdf 不是媒体", "a.pdf", "application/pdf", KindUnknown},
		{"txt 不是媒体", "a.txt", "text/plain", KindUnknown},
		{"伪装成视频的 txt 不是视频", "a.mp4.txt", "text/plain", KindUnknown},
		{"没有扩展名时看 MIME", "noext", "video/mp4", KindVideo},
		{"没有扩展名也没有 MIME", "noext", "", KindUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			assert.Equal(t, c.want, Classify(c.fileName, c.ct))
		})
	}
}

// TestParseDurationMs 特征:时长字符串解析。
func TestParseDurationMs(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"12.500000", 12500},
		{"61", 61000},
		{"0.001", 1},
		{"N/A", 0},
		{"", 0},
		{"-3.0", 0},
		{"abc", 0},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			assert.Equal(t, c.want, parseDurationMs(c.in))
		})
	}
}

// indexOf 找参数下标
func indexOf(args []string, want string) int {
	for i, a := range args {
		if a == want {
			return i
		}
	}
	return -1
}
