# media —— 音视频探测与转码

> 上传的视频/音频要能在列表里看到**时长**和**封面**,点开要能**在浏览器里播**。
> media 只做这两件事:用 ffprobe 读元信息、用 ffmpeg 抽帧和转码。

## 功能

| 能力 | 函数 | 干什么 |
|---|---|---|
| 探测 | `Probe` | ffprobe 读时长 / 分辨率 / 容器 / 编码 |
| 抽封面 | `ExtractFrame` | ffmpeg 抽一帧存成 jpg(缩到 640 宽) |
| 转码 | `Transcode` | 转成浏览器都播得了的 H.264 + AAC / MP4 |
| 判能不能播 | `BrowserPlayable` / `UnplayableReason` | "ffmpeg 能解" ≠ "浏览器能播" |
| 判是不是媒体 | `Classify` | 按文件名 + Content-Type 判 video / audio |
| 纯函数工具 | `NormalizeContainer` / `FramePosition` / `PlaybackExt` / `TranscodeArgs` / `PlaybackSizeHint` | 不碰外部命令,单独可测 |

### 边界:这个包只管跑命令、解析输出

它**不认识数据库、不认识存储、不认识用户** —— 那是 service 层的事。
分成两层的原因:命令怎么拼、输出怎么解析是**纯逻辑**,可以脱离 ffmpeg 测;
而"什么时候跑、结果存哪"是业务编排,测它就得把整个 service 搭起来。

### ⚠️ 工具不可用 ≠ 报错

开发机上没装 ffmpeg 是常态(ffmpeg 不是 Go 依赖,不会随仓库分发)。
所以"找不到二进制"是一个**可识别的哨兵错误** `ErrUnavailable`,由调用方决定降级 ——
上传照常成功,只是这个文件没有时长和封面。把它当成"探测失败"泛化处理的话,
日志里每次上传都会刷一条 error,而用户看不出任何区别(前端就是少个时长)—— 那是噪音。

### ⚠️ 命令执行器是注入的

`Runner` 接口让测试可以塞一个假执行器进去,断言**命令怎么拼、输出怎么解析**,
不需要真的装 ffmpeg。真实的 ffmpeg 集成测试是**另一件事**(装了 ffmpeg 才跑,
没装就跳过 —— 跳过的那条会在测试名里写明原因)。

## 配置

**该包没有独立的 `media:` yaml 块** —— 媒体相关的键都在 `file:` 块里
(`internal/config/file.go` 的 `FileConfig`):

```yaml
file:
  # ---- 媒体探测(视频时长 / 封面帧)----
  ffmpeg_path: ffmpeg          # ffmpeg 可执行文件路径(默认走 PATH)
  ffprobe_path: ffprobe        # ffprobe 可执行文件路径(默认走 PATH)
  media_max_size_mb: 2048      # 媒体探测的文件大小上限。0 = 不限

  # ---- 转码 ----
  transcode_enabled: true      # 布尔零值是 false = 关,必须显式写 true
  transcode_workers: 2         # 转码并发数
```

| 键 | 类型 | 默认 | 说明 |
|---|---|---|---|
| `file.ffmpeg_path` | string | `ffmpeg` | ffmpeg 可执行文件路径,默认走 PATH |
| `file.ffprobe_path` | string | `ffprobe` | ffprobe 可执行文件路径,默认走 PATH |
| `file.media_max_size_mb` | int | `2048` | 媒体探测的文件大小上限(MB),`0` = 不限 |
| `file.transcode_enabled` | bool | Go 零值 `false`(= 关),yaml 模板里显式写 `true` | 是否给"浏览器播不了"的音视频转一份可播副本 |
| `file.transcode_workers` | int | `2` | 转码并发数 |

### ⚠️ 环境变量优先于 yaml

`FFMPEG_PATH` / `FFPROBE_PATH` **优先**于上面两个路径键
(`FileConfig.ResolveMediaTools`)。

ffmpeg 不是 Go 依赖,不随仓库分发,每个部署环境装的位置都不一样
(Windows 上常常是某个绿色版目录)。**yaml 是仓库里的默认值,环境变量才是
"这台机器上它到底在哪"的唯一真相。**

### 代码侧

```go
media.Probe(ctx, ffprobeBin, path) (*Info, error)
media.ExtractFrame(ctx, ffmpegBin, in, outPath string, atMs int64) error
media.Transcode(ctx, ffmpegBin, in, out string, kind Kind) error
media.DefaultRunner Runner        // 默认执行器(真的 exec)
```

`Runner` 接口可替换:

```go
type Runner interface {
    Run(ctx context.Context, name string, args ...string) ([]byte, error)
}
```

## 使用方法

完整示例:`examples/media/main.go` —— 本示例只演示**纯函数**部分,
真正转码需要本机装 ffmpeg / ffprobe。

### 种类判定

```go
k := media.Classify(c.fileName, c.contentType)   // media.KindVideo / KindAudio / KindUnknown
```

### 容器归一化

```go
media.NormalizeContainer("mov,mp4,m4a,3gp,3g2,mj2")   // "mp4"
media.NormalizeContainer("matroska,webm")              // "webm"
```

### 浏览器可播判定

```go
reason := media.UnplayableReason(info)   // 空字符串 = 能播
media.BrowserPlayable(info)              // 同一判据的布尔版
```

### 播放副本的扩展名

```go
media.PlaybackExt(media.KindVideo)   // ".mp4"
media.PlaybackExt(media.KindAudio)   // ".m4a"
```

### 转码参数

```go
for _, a := range media.TranscodeArgs(kind, "in.xxx", "out.yyy") {
    fmt.Println(a)
}
```

### 跑命令的三个函数(签名)

```go
info, err := media.Probe(ctx, ffprobePath, localPath)              // 时长/分辨率/容器/编码
err = media.ExtractFrame(ctx, ffmpegPath, localPath, outPath, atMs) // 抽封面
err = media.Transcode(ctx, ffmpegPath, localPath, outPath, kind)    // 转可播副本
```

`atMs` 的落点由 `media.FramePosition(durationMs)` 给(见下)。

## 注意事项

- ⚠️ **ffmpeg / ffprobe 不在 PATH 时的行为**:`exec.CommandContext` 找不到二进制
  ⇒ 错误能被 `errors.Is(err, exec.ErrNotFound)` 认出 ⇒ 包把它翻译成
  **`ErrUnavailable`**。调用方**必须用 `errors.Is` 认它** —— "没装 ffmpeg"(部署状态)
  和"文件解不开"(文件本身有问题)是两件完全不同的事,混在一起运维看到满屏 error
  会以为文件坏了。

- ⚠️ **`ErrUnavailable` 不是"探测失败",业务照常成功**。service 层把它记成
  **Info 而不是 Warn**:每个媒体文件上传都打一条 Warn 是噪音,而用户看不出任何区别。
  另外"ffprobe 没配"是**整个不探**(不是"跑一次看看报什么错")——
  只配了 ffmpeg 没配 ffprobe 是常见形态(精简包),那时错误信息是 "exec: no command",
  看的人完全猜不到是配置缺了。

- ⚠️ **没装 ffmpeg 也能正常上传** —— 探测是锦上添花,失败只是列表里少一个时长和一张封面。

- ⚠️ **ffmpeg 能解 ≠ 浏览器能播**。WMV / FLV / MKV / MPEG-4 Part 2 这些格式
  ffmpeg 全都能解,于是时长探得出来、封面也抽得出来 —— **看起来一切正常**,
  但用户点开直链,浏览器是把它下载下来而不是播。"看起来正常但其实播不了"是最坏的
  一种状态:没有报错、没有提示。

- ⚠️ **判据是"全平台通吃",不是"某个浏览器能播"**。Safari 不支持 Ogg/Vorbis、
  Firefox 不支持 HEVC、Chrome 对 HEVC 依平台 —— 只要"有一个主流浏览器播不了",
  这个文件就不能算"浏览器可播"。

- ⚠️ **容器和编码是两件事,都要查**。MKV 里面装 H.264 —— 编码支持,但浏览器不解
  MKV 容器 ⇒ 还是播不了;MP4 容器里装 AC-3 ⇒ 画面能看,声音要装 Dolby 插件。
  **视频的音轨也要查**。

- ⚠️ **判不出来 = 放行**。`Info` 里任何一项为空(比如 ffprobe 没吐 `format_name`),
  `BrowserPlayable` 就不拦。别把"空"当成"浏览器一定不能播" —— 转码要烧 CPU、占存储,
  "拿不准"不该触发它。

- ⚠️ **纯 mkv 的 `format_name` 只有 `"matroska"`,webm 是 `"matroska,webm"`**
  ⇒ 判 webm 必须看**整串里有没有 webm**,不能只看第一项(第一项对两者都是 matroska)。
  `NormalizeContainer` 里 webm 必须排在 matroska 前面判。

- ⚠️ **`Classify` 扩展名当主判据,MIME 只兜底**。`mime.TypeByExtension` 在 Windows 上
  会去查注册表,`.mkv` / `.flac` 这种冷门格式**不一定**有值,查不到就落到
  `application/octet-stream` ⇒ 视频被判成"不是媒体" ⇒ 不抽封面。
  反过来:**扩展名认出来了但不在媒体名单里 ⇒ 就是不是媒体**,别再拿 content_type 兜底
  —— 一个叫 `a.mp4.txt` 的文本文件会被浏览器报成 `text/plain`,而用户眼里它就是个 txt。

- ⚠️ **时长 `"N/A"` 当 0,不是错误**。某些容器(尤其 mpeg-ts / 部分 mp4)
  `format.duration` 是 `"N/A"`,此时兜底取**视频流**的 duration。
  负时长也当 0(坏数据)。

- ⚠️ **封面落点不是 0**。`FramePosition` 的规则:≤ 0 → 0;< 3 秒 → 取中点;
  ≥ 3 秒 → 取 10% 但不超过 3 秒。很多视频开头是黑屏/片头,抽出来是一片黑,
  用户看到的就是"这个视频没有封面"。

- ⚠️ **抽帧用输入端 seek**(`-ss` 放在 `-i` **前面**)= 快但可能不准(跳到最近的关键帧)。
  对"抽一张封面"完全够用;放在 `-i` 后面才准,代价是要解完整个文件。

- ⚠️ **文件大小闸在 service 层,不在本包**。`file.media_max_size_mb`(默认 2048)
  决定要不要探测 —— 因为**探测要把文件完整落到本地磁盘**(ffmpeg 只能读本地文件),
  所以这个闸 = "临时盘要留多少空间"。`0` = 不限。

- ⚠️ **`transcode_enabled` 的布尔零值是 false = 关** —— yaml 模板里必须显式写
  `true`,否则"配了 ffmpeg 却不转码"这件事**没有任何提示**。
  关掉它只是不转码,别的照旧(时长/封面照旧,列表里会显示"浏览器不支持" + 原因)。

- ⚠️ **每个转码是一个 ffmpeg 进程,吃满一个核** —— `transcode_workers` 就是
  "最多同时烧几个核"。默认 2 是刻意的:够一个上传高峰用,又不会把一台 2 核的
  小机器打满。

- ⚠️ **转码参数是显式列出来的**(`TranscodeArgs`),不是拼字符串 —— 拼字符串意味着
  参数来自文件名时会有注入面。其中 `-map` 必须显式给(不给会把字幕轨和数据轨也搬
  过去,mp4 装不下,报错是 "Could not find tag for codec … in stream #1",极难一眼看懂),
  `-movflags +faststart` 让 moov atom 放文件头(否则浏览器必须把整个文件下完才起播)。

- ⚠️ **转码产物不一定更小**。H.264 CRF 23 的产物通常比原文件小(尤其 WMV/FLV/
  MPEG-4 Part 2 这些编码效率低的格式),但**低码率的 DivX 反过来会变大** ——
  不能假设"转码完一定更小"。

- ⚠️ **音频不抽帧**。音频的"封面"是图标(前端按扩展名画),抽出来的波形图对一张唱片封面
  没有意义。service 层只在 `Kind == KindVideo` 时才调 `ExtractFrame`。

## 相关

- 源码:`internal/pkg/media/`(`media.go` 探测与抽帧,`playback.go` 可播判定与转码)
- 可运行示例:`examples/media/main.go` —— `go run ./examples/media`
- 文件落在哪(直链 / key 形态)→ [oss](oss.md)
- 媒体工具路径优先级:环境变量 `FFMPEG_PATH` / `FFPROBE_PATH` > yaml `file.ffmpeg_path` / `file.ffprobe_path`
