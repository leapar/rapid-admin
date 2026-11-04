// 媒体探测的**真机**测试(真的 ffmpeg)(M141)
//
// ⚠️ **跟 media_test.go 是两回事** —— 那条线用假执行器,断言的是
// "命令怎么拼、输出怎么解析";这条线**真的调 ffmpeg**,验的是
// "参数拼出来 ffmpeg 认不认"(版本差异、参数改名、输出格式变了,
// 都只有真跑才暴露)。
//
// ⚠️ **没装 ffmpeg 就跳过,而且跳过原因写在测试名里** ——
// `t.Skip` 之后 CI 不会说"这条没跑",但日志里能看到。
// ⚠️ **跳过是最容易骗人的形态** —— 所以这条用例的价值恰恰在于
// "装了 ffmpeg 的机器上它必须真的跑过",所以下面会断言 ffmpeg 的版本,
// 让"装了个假的/残缺的 ffmpeg"也露出来。
package media

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// requireFFmpeg 找到 ffmpeg/ffprobe,找不到就跳过。
//
// ⚠️ **跳过时把"去哪儿装"写进消息里** —— 否则看到 skip 的人不知道
// 是环境缺东西还是用例坏了。
func requireFFmpeg(t *testing.T) (ffmpeg, ffprobe string) {
	t.Helper()
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skipf("跳过:本机没有 ffmpeg(装一个:winget install Gyan.FFmpeg,或从 gyan.dev/ffmpeg/builds 下静态包)")
	}
	ffprobe, err = exec.LookPath("ffprobe")
	if err != nil {
		t.Skipf("跳过:本机没有 ffprobe(ffmpeg 静态包里通常自带)")
	}
	return ffmpeg, ffprobe
}

// makeTestVideo 用 ffmpeg 造一个 3 秒的测试视频。
//
// ⚠️ **用 lavfi 的 testsrc 而不是放一个二进制样本进仓库** ——
// 样本文件进仓库会让 clone 变大,而 testsrc 是 ffmpeg 自带的合成源,
// 在任何版本上都能造出同一个视频。
func makeTestVideo(t *testing.T, ffmpeg, path string, seconds int) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-nostdin", "-y",
		"-f", "lavfi",
		"-i", "testsrc=duration=3:size=320x240:rate=10",
		"-c:v", "libx264",
		"-pix_fmt", "yuv420p",
		path,
	)
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "造测试视频失败:%s", string(out))
}

// TestIntegration_ProbeRealVideo 特征:
// **真的 ffmpeg 能解出时长** —— 3 秒的测试视频,探测结果应当是 3000ms 上下。
func TestIntegration_ProbeRealVideo(t *testing.T) {
	ffmpeg, ffprobe := requireFFmpeg(t)
	dir := t.TempDir()
	vid := filepath.Join(dir, "sample.mp4")
	makeTestVideo(t, ffmpeg, vid, 3)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := Probe(ctx, ffprobe, vid)
	require.NoError(t, err)
	assert.Equal(t, KindVideo, info.Kind)
	// ⚠️ **给容差,不给精确值** —— 不同 ffmpeg 版本的时长舍入不完全一样
	assert.InDelta(t, 3000, info.DurationMs, 500)
	assert.Equal(t, 320, info.Width)
	assert.Equal(t, 240, info.Height)
}

// TestIntegration_ExtractRealFrame 特征:
// **真的 ffmpeg 能抽出一帧 jpg**,而且那是一张真图片(不是 0 字节)。
func TestIntegration_ExtractRealFrame(t *testing.T) {
	ffmpeg, ffprobe := requireFFmpeg(t)
	dir := t.TempDir()
	vid := filepath.Join(dir, "sample.mp4")
	makeTestVideo(t, ffmpeg, vid, 3)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := Probe(ctx, ffprobe, vid)
	require.NoError(t, err)

	out := filepath.Join(dir, "cover.jpg")
	require.NoError(t, ExtractFrame(ctx, ffmpeg, vid, out, FramePosition(info.DurationMs)))

	st, err := os.Stat(out)
	require.NoError(t, err, "封面文件必须真的落盘")
	assert.Greater(t, st.Size(), int64(100), "封面不该是空文件")

	// ⚠️ **内容必须真是 JPEG** —— ffmpeg 有可能因为参数问题写出别的格式
	//   (比如 PNG),那前端 <img> 照样能显示,但"抽的是 jpg"这个约定就破了。
	head, err := os.ReadFile(out)
	require.NoError(t, err)
	require.Greater(t, len(head), 3)
	assert.Equal(t, []byte{0xFF, 0xD8, 0xFF}, head[:3], "必须是 JPEG 魔数")
}
