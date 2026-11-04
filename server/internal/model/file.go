// 文件系统模块 —— 文件夹 / 文件 / 上传会话
// 设计见 docs/文件系统模块设计方案V1.0.md §4
//
// ⚠️ **这三张表里没有一张存"文件内容"**。内容在 local 磁盘或 OSS 上,表里只存
// key 和元信息。这不是缓存,是**唯一真相源**的分工:存储层管字节,DB 管归属
// 和配额。搞反了就会出现"文件删了 DB 还在"(配额永远退不回来)。
package model

import "time"

// ============== 枚举 ==============

// 存储后端
type FileStorage int

const (
	FileStorageLocal FileStorage = 1 // 本地磁盘
	FileStorageOSS   FileStorage = 2 // 阿里云 OSS
)

// 文件夹 / 文件状态
//
// ⚠️ **和 `BaseModel.DeletedAt` 的软删除是两套东西,别混**。
// `DeletedAt` 是 GORM 的软删除作用域,所有查询自动加 `deleted_at IS NULL`;
// 而这里的 `Status` 是**业务状态**,用来表达"文件已被标记删除但还没真正清掉
// 存储"这类中间态。两者可以并存(Status=0 且 DeletedAt 有值 = 已进回收站)。
type FileStatus int

const (
	FileStatusNormal  FileStatus = 0 // 正常
	FileStatusDeleted FileStatus = 1 // 已删(等清理任务回收存储)
	FileStatusPending FileStatus = 2 // 上传中,还没落成完整对象
)

// 上传会话状态
type UploadSessionStatus int

const (
	UploadSessionActive   UploadSessionStatus = 0 // 进行中
	UploadSessionDone     UploadSessionStatus = 1 // 已完成
	UploadSessionCanceled UploadSessionStatus = 2 // 已取消
	UploadSessionExpired  UploadSessionStatus = 3 // 已过期
)

// 业务段(biz)校验见 oss.ValidateBiz —— 这里只做 DB 侧的 index,不重复校验规则。

// ============== 转码状态 ==============

// TranscodeStatus 转码状态(M145)。
//
// ⚠️ **0 是"不需要转码",不是"还没转"** ——
// 绝大多数文件浏览器本来就能播(H.264/MP4 是绝对主流),
// 所以绝大多数行都停在 0。把它当成"待办"的话,前端会给每个文件都显示
// "待转码",而绝大多数文件其实压根不需要转。
//
// ⚠️ **它是"业务状态"不是"软删除标记"** —— 和 FileStatus 一样,
// 跟 BaseModel.DeletedAt 是两套东西,别混。
type TranscodeStatus int

const (
	// TranscodeStatusNone 不需要转码(浏览器能直接播)
	TranscodeStatusNone TranscodeStatus = 0
	// TranscodeStatusPending 待转码(已判定需要转,等 worker 接手)
	TranscodeStatusPending TranscodeStatus = 1
	// TranscodeStatusRunning 转码中
	TranscodeStatusRunning TranscodeStatus = 2
	// TranscodeStatusDone 转码完成(此时 PlaybackKey 有值)
	TranscodeStatusDone TranscodeStatus = 3
	// TranscodeStatusFailed 转码失败(此时 PlaybackKey 为空)
	TranscodeStatusFailed TranscodeStatus = 4
)

// ============== sys_folder ==============

// SysFolder 文件夹。
//
// ⚠️ **扁平结构**:`ParentID` 字段保留但**恒为 0**(设计文档 §2.7)。需求的路径
// 规范里没有父文件夹层。将来若要支持 A/B/C 三级,改的是 key 生成逻辑而不是
// 表结构 —— 但仍要评估一次全量数据迁移,所以这个字段先占位。
//
// ⚠️ **唯一索引不含 deleted_at**(设计文档 §4.1 原写的是
// `unique(user_id, biz, name, deleted_at)`,**那条是错的**)。SQL 里 UNIQUE 索引
// 认为所有 NULL 互不相等,而未删除的行 `deleted_at` 正是 NULL —— 所以加了
// deleted_at 之后,活跃行之间的唯一性**根本不生效**:实测同一 (user_id, biz,
// name) 连插两条都成功。而且它失败得很安静,不报任何错。
//
// 代价(要一起记住):软删之后文件夹名**不会自动释放**,重建同名文件夹会撞唯一
// 索引。**M80 的 DeleteFolder 必须在软删时把 Name 改成带 id 后缀的形式**
// (如 `默认文件夹#del:123`)。这条不做的话,用户"删掉再传"就传不进去了。
type SysFolder struct {
	BaseModel
	// UserID 归属人。
	//
	// ⚠️ **这一列是联合唯一索引的第一段,少写就会变成全局唯一**。索引名叫
	// `uk_folder_owner_biz_name`("owner" = user_id),但如果 UserID 上没有
	// `uniqueIndex` tag,gorm 只会拿另外两段建索引 —— 名字骗人,行为是
	// `UNIQUE(biz, name)`。后果:用户 2 建"默认文件夹"时撞上用户 1 的,报
	// duplicate key,而报错完全看不出跟哪个用户有关。
	UserID uint64 `gorm:"index;not null;uniqueIndex:uk_folder_owner_biz_parent_name,priority:1" json:"user_id,string"`
	// Biz 业务段。**和 UserID / ParentID / Name 一起进联合唯一索引** —— 同一用户
	// 同一业务同一父级下不能有两个同名文件夹,否则"默认文件夹"的懒创建会造出两个来。
	Biz string `gorm:"size:32;not null;uniqueIndex:uk_folder_owner_biz_parent_name,priority:2" json:"biz"`
	// Name 文件夹名(展示用,不进 key)
	Name string `gorm:"size:128;not null;uniqueIndex:uk_folder_owner_biz_parent_name,priority:4" json:"name"`
	// ParentID 上级文件夹 id,0 = 根级。
	//
	// ⚠️ **2026-10-30(M125)之前这一列恒为 0** —— 设计文档 §2.7 D7 明确写了
	// "文件夹是扁平的,不支持嵌套"。M125 起它**真正启用**:文件夹可以建子文件夹,
	// 点文件夹名进入其子级。
	//
	// ⚠️ **它进了唯一索引** ⇒ 索引是 (user_id, biz, parent_id, name) ——
	// 同名文件夹**只要不在同一个父级下就允许**(A/图纸 和 B/图纸 能共存)。
	// 2026-11-03 起不再有老索引迁移:M125 之前的 (user_id, biz, name) 旧索引
	// 只存在于更早建出来的库里,新库由 AutoMigrate 直接建出四列索引。
	ParentID uint64 `gorm:"index;not null;default:0;uniqueIndex:uk_folder_owner_biz_parent_name,priority:3" json:"parent_id,string"`
	// CoverKey 封面 key。
	//
	// ⚠️ **每文件夹一张,不是每文件一张**(设计文档 §3.4,用户在需求澄清时定的)。
	// 由 `POST /folders/:id/cover` 关联,关联的是某个 `sys_file` 的 id,而不是
	// 直接写 key —— 写 key 的话封面文件一删就变成悬空 key,没有任何东西会告诉你。
	CoverKey string `gorm:"size:512" json:"cover_key"`
	// CoverFileID 封面对应的 sys_file.id,0 = 无封面
	CoverFileID uint64 `gorm:"index;default:0" json:"cover_file_id,string"`
	// FileCount / TotalSize 冗余计数,列表页免 JOIN。
	//
	// ⚠️ **冗余就意味着会漂移**。删文件时必须同事务更新这两个字段,否则列表页
	// 显示的"12 个文件 3.4MB"和真实值对不上,而且没有任何报错。要对账就定时
	// `COUNT(*) / SUM(file_size)` 回写(设计文档 R2)。
	FileCount int   `gorm:"default:0" json:"file_count"`
	TotalSize int64 `gorm:"default:0" json:"total_size"`
	// IsDefault 是懒创建出来的默认文件夹
	//
	// ⚠️ 默认文件夹**不能删**,也不能改名 —— 前端菜单和"上传"按钮都按它定位。
	IsDefault bool       `gorm:"default:false;index" json:"is_default"`
	Status    FileStatus `gorm:"default:0;index" json:"status"`
}

func (SysFolder) TableName() string { return "sys_folder" }

// ============== sys_file ==============

// SysFile 文件元信息。
type SysFile struct {
	BaseModel
	FolderID uint64 `gorm:"index;not null" json:"folder_id,string"`
	// UserID / Biz 冗余自 Folder,列表页免 JOIN。
	//
	// ⚠️ 冗余字段的第二份真相:改文件夹归属时**必须同步更新这里**,否则会按
	// 旧归属判权限。M80 的归属校验一律读这一份(单表查询,不用 JOIN),所以它
	// 是**权限判定的依据**,不是可有可无的缓存。
	UserID uint64 `gorm:"index;not null" json:"user_id,string"`
	Biz    string `gorm:"size:32;index;not null" json:"biz"`

	// FileKey 对象存储里的完整 key。
	//
	// ⚠️ **size:512,不是 255**。`rapid-admin/` + 19 位 uid + `/` + biz + `/` + 19 位
	// fid + `/` + 16 字符前缀 + `/` + 180 字节原名 ≈ 260 字节,**已经超 255**。
	// 同表 `biz_digital_asset.file_key` 是 255,别拿那个的长度来套。
	//
	// ⚠️ **唯一索引是本表最重要的一条约束**。file_key 是**指向存储里那份字节的
	// 唯一把手**;两行共用一个 key,删其中一行就会把对象删掉,另一行变成指向空气
	// 的悬空记录 —— 而且不报错,直到用户点下载才 404。
	//
	// (InnoDB 单索引上限 3072 字节,512 * utf8mb4 = 2048,放得下。)
	FileKey string `gorm:"size:512;not null;uniqueIndex:uk_sys_file_key" json:"file_key"`
	// FileName 原始文件名,下载时还原(Content-Disposition 用)
	//
	// ⚠️ 255 是**字节数不是字符数**。中文文件名 255 字节 ≈ 85 个字,而 iOS
	// 允许 255 字符。超限的上传在 M80 的 service 层就要被拒,否则会静默截断。
	FileName string `gorm:"size:255;not null" json:"file_name"`
	// FileSize 字节数
	FileSize int64 `gorm:"default:0" json:"file_size"`
	// ContentType MIME。local 存的是**探测**出来的(按扩展名),不是浏览器
	// 上报的值 —— 上报值是客户端说了算的。
	ContentType string `gorm:"size:128" json:"content_type"`
	// ETag 校验值。
	//
	// ⚠️ **跨 provider 语义不同**:local 是内容的 md5 十六进制;OSS 是分片
	// 合并的 MD5(带引号,已在 aliyun provider 里剥掉)。所以它**不是**跨
	// provider 可比的校验和,只用于"同一 key 的内容有没有变"。
	ETag string `gorm:"size:128" json:"etag"`
	// Storage 1 local / 2 oss
	//
	// ⚠️ **为什么每行都要存而不是全局一个配置值**:迁移期间两种 provider 会
	// 共存(先复制再切),按行记才知道这个文件实际在哪。读取时以它为准。
	Storage FileStorage `gorm:"default:1;index" json:"storage"`
	// IsCover 是否被某个文件夹当作封面引用
	//
	// ⚠️ 是**引用标记**不是所有权。删文件夹封面不等于删文件。
	IsCover bool       `gorm:"default:false;index" json:"is_cover"`
	Status  FileStatus `gorm:"default:0;index" json:"status"`

	// DurationMs 媒体时长(毫秒)。0 = 不是媒体 / 没探测出来。
	//
	// ⚠️ **不是媒体和"探测失败"都落在 0 上** —— 前端看到 0 就显示不出时长,
	//   而这两件事对用户是同一件事(就是"没时长")。要区分"是不是媒体"
	//   看 content_type 就够。
	//
	// ⚠️ **单位是毫秒不是秒** —— 短视频(几百毫秒)在秒单位下会被截成 0,
	//   看起来跟"没有时长"一模一样。
	DurationMs int64 `gorm:"default:0" json:"duration_ms"`

	// CoverKey 封面帧的存储 key(视频抽出来的第一帧,jpg)。
	//
	// ⚠️ **不是 sys_file 的自引用** —— 封面帧**不是**用户文件:
	//   它不进文件列表、不计文件数、不占用户配额(它比原视频小得多,
	//   但"用户传了个 2GB 视频,配额却按 2GB+算"是不能接受的)。
	//   所以它是**一个对象 + 一个 key**,不是一行。
	//
	// ⚠️ **空串 = 没有封面** —— 音频没有封面(前端摆音频图标)、
	//   没装 ffmpeg、探测失败,都是空串。
	CoverKey string `gorm:"size:512" json:"cover_key"`

	// MediaContainer 原始容器名(mp4 / webm / mkv / avi / asf / flv …)。
	//
	// ⚠️ **记的是"原始"的,不是转码后的** —— 它回答的是"用户传上来的是什么",
	//   而 PlaybackKey 回答的是"我们转成了什么"。两个都要留:
	//   只留后者就看不出"为什么当初要转"。
	MediaContainer string `gorm:"size:32" json:"media_container"`
	// MediaVideoCodec 原始视频编码(h264 / hevc / vp9 / mpeg4 / wmv3 …)。
	MediaVideoCodec string `gorm:"size:32" json:"media_video_codec"`
	// MediaAudioCodec 原始音频编码(aac / mp3 / opus / wmav2 …)。无音轨 = 空。
	MediaAudioCodec string `gorm:"size:32" json:"media_audio_codec"`
	// MediaReason 为什么浏览器播不了(一句人话)。空串 = 浏览器能直接播。
	//
	// ⚠️ **这是给人看的,不是给机器判的** —— 机器判据在 pkg/media 里。
	//   这里存一份是因为用户会问"为什么我的视频播不了",
	//   而让前端自己拿 container/codec 去拼这句话 = 前端多一份判据表,
	//   后端改一次就得同步改两处。
	MediaReason string `gorm:"size:128" json:"media_reason"`

	// PlaybackKey 浏览器可播副本的存储 key(M145)。
	//
	// ⚠️ **原文件永远保留** —— 转码是**追加**一份,不是替换。
	//   用户下载到的还是他传上来的那个文件,只是多了一个"能直接播"的版本。
	//
	// ⚠️ **它和 CoverKey 一样不是 sys_file 的自引用** —— 不进文件列表、
	//   不计文件数、不占用户配额(理由同 CoverKey:它是派生产物,
	//   "用户传了个 2GB 视频,配额却按 2GB+算"是不能接受的)。
	PlaybackKey string `gorm:"size:512" json:"playback_key"`

	// TranscodeStatus 转码状态(见 TranscodeStatus 的注释)。
	TranscodeStatus TranscodeStatus `gorm:"default:0;index" json:"transcode_status"`
}

func (SysFile) TableName() string { return "sys_file" }

// ============== sys_upload_session ==============

// SysUploadSession 分片上传会话。
//
// ⚠️ **为什么必须有这张表**:OSS 的 ListParts 能告诉你"哪些分片传了",但
// **不告诉你文件多大、属于谁、传到哪一步**。断点续传靠它把会话接上。
// local 更依赖它 —— 本地磁盘没有分片概念,临时文件在哪只有它记着。
type SysUploadSession struct {
	BaseModel
	// UploadID OSS 的 UploadID / local 的临时会话名
	UploadID string `gorm:"size:128;index" json:"upload_id"`
	// FileKey 目标 key
	FileKey  string `gorm:"size:512;not null" json:"file_key"`
	UserID   uint64 `gorm:"index;not null" json:"user_id,string"`
	FolderID uint64 `gorm:"index;not null" json:"folder_id,string"`
	FileName string `gorm:"size:255" json:"file_name"`
	FileSize int64  `gorm:"default:0" json:"file_size"`
	// ContentType 初始化时定的目标类型
	ContentType string `gorm:"size:128" json:"content_type"`
	// PartSize 分片大小。⚠️ 存下来而不是每次重算默认值:默认值以后可能改,
	// 而**已上传的分片是按旧 partSize 切的**,改了偏移全错位。
	PartSize int64               `gorm:"default:0" json:"part_size"`
	Status   UploadSessionStatus `gorm:"default:0;index" json:"status"`
	// QuotaHeld 这次会话**预扣**了多少配额字节
	//
	// ⚠️ **过期清理时靠它精确回滚**(设计文档 §4.5)。只存个标志位的话,
	// 清理任务就得靠 file_size 猜,猜错就是配额凭空少一截或多一截。
	QuotaHeld int64 `gorm:"default:0" json:"quota_held"`
	// ExpiresAt 过期时刻 = 创建时刻 + session_ttl
	//
	// ⚠️ 存**绝对时刻**而不是"创建时间 + 配置值":配置随时可能改,而清理任务
	// 要判的是"按当初的约定它该不该过期"。存相对量等于让清理结果取决于清理时
	// 读到的配置值。
	ExpiresAt time.Time `gorm:"index" json:"expires_at"`

	// MediaTmpPath 媒体探测用的**本地副本**路径(分片上传时用)。
	//
	// ⚠️ **为什么分片要留本地副本** —— ffmpeg 只能读本地文件,
	//   而分片是分批传到 OSS 的,不落一份本地副本就没法探测时长/抽封面。
	//   单次中转(Receive)那条路是边传边写临时文件,不用存路径。
	//
	// ⚠️ **存绝对路径** —— 相对路径在进程 cwd 变化后就不是同一个文件了,
	//   而"合并分片"可能发生在**另一个进程**(多副本部署)。
	//   拿不到副本时(别的副本合并、机器重启后文件被清)⇒ 跳过探测,
	//   **不影响上传成功**。
	MediaTmpPath string `gorm:"size:512" json:"media_tmp_path"`
}

// TableName
func (SysUploadSession) TableName() string { return "sys_upload_session" }

// IsExpired 这个会话是不是过期了(now 显式传入,便于测试)
func (s *SysUploadSession) IsExpired(now time.Time) bool {
	return !s.ExpiresAt.IsZero() && !now.Before(s.ExpiresAt)
}

// Done 这个会话是不是已经终结(完成 / 取消 / 过期)。
//
// 终结的会话**不该再被清理任务碰** —— 对它调 Abort 会报 "NoSuchUpload",
// 而那是一条无意义的错误日志,量大了会把真错误淹掉。
func (s *SysUploadSession) Done() bool { return s.Status != UploadSessionActive }
