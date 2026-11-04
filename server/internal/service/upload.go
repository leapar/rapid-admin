// UploadService —— 分片上传会话(设计文档 §8.2 / §9)
//
// ⚠️ **这个 service 管的是"会话",不是"字节"**。字节的读写一律委托
// `oss.MultipartProvider`;这里负责的是只有 DB 知道的那部分:会话归属、
// 预扣配额、断点续传的依据、过期回滚。
//
// 之所以单独一个 service 而不是塞进 FileService:FileService 的每个方法
// 都在"一次请求内完成",而分片上传横跨多次请求,状态必须在 DB 里活下来。
package service

import (
	"context"
)

// MultipartInitReq 初始化分片上传
type MultipartInitReq struct {
	// FolderID 目标文件夹,0 = 该 biz 下的默认文件夹
	FolderID uint64 `json:"folder_id,string"`
	Biz      string `json:"biz"`
	// FileName 原始文件名
	FileName string `json:"file_name"`
	// FileSize 总字节数。**必填且必须与实际一致** —— local 模式靠它在
	// Complete 阶段校验合并结果(oss.InitOpts.TotalSize 的注释解释了为什么)
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type"`
	// PartSize 客户端自选分片大小,0 = 用配置默认值。
	// ⚠️ **只在 init 时生效**:已经传上去的分片是按这个大小切的,
	// 后续 complete 换任何值都会错位
	PartSize int64 `json:"part_size"`
}

// MultipartInitResp 初始化结果
type MultipartInitResp struct {
	// SessionID 会话 id(前端后续所有分片请求都带这个)
	SessionID uint64 `json:"session_id,string"`
	// Key 目标 key(前端不需要自己拼,但要知道落在哪)
	Key string `json:"key"`
	// PartSize 实际生效的分片大小
	//
	// ⚠️ **前端必须按这个值切,不能按自己请求里带的值切**。服务端可能因为
	// "片数会超过 10000 片"而把片长抬大(见 planParts),而 resp 里这个值
	// 才是权威的。超大文件上按错值切,合并出来的内容顺序会全乱。
	PartSize int64 `json:"part_size"`
	// PartNum 总分片数
	PartNum int `json:"part_num"`
	// ExpiresAt 过期时刻(unix 秒)
	ExpiresAt int64 `json:"expires_at"`
}

// UploadPartReq 上传一个分片
type UploadPartReq struct {
	// SessionID 会话
	SessionID uint64
	// PartNumber 分片号(1 起)
	PartNumber int
	// Body 分片内容。⚠️ **必须限长**(见 §9.3):本仓没有内存池,
	// 没有显式上限的话 GC 会成为唯一防线
	Body []byte
}

// PartInfo 分片信息
type PartInfo struct {
	PartNumber int    `json:"part_number"`
	ETag       string `json:"etag"`
	Size       int64  `json:"size"`
}

// CompleteReq 完成分片上传
//
// ⚠️ **不带分片清单** —— 服务端一律从存储的 ListParts 取权威清单。
// 设计文档 §8.2 原本写的是 `body: [{part_number, etag}]`,实现时改掉了,
// 更正理由见该节 2026-10-22 的更正块:客户端上报的分片清单是分片上传的
// 经典事故源(漏报一片 → 本地静默合并出少一片的文件;多报一片 → 合并结果
// 不可预期),而服务端本来就查得到,多一次 ListParts 调用换掉一整类错误很划算。
type CompleteReq struct {
	// SessionID 会话
	//
	// ⚠️ **字段名和 `,string` 都必须写**:前端 POST 的是 `{"session_id":"123"}`,
	// 值是**字符串**(它来自 MultipartInitResp.SessionID,那边带 `,string`,
	// 序列化出来就是 `"123"`)。少字段名 → 下划线匹配不上,静默解成 0;
	// 少 `,string` → 字符串解不进 uint64,直接 400。两个都少的话症状是
	// "分片全传完了,合并永远失败"。跟 MultipartInitReq.FolderID 同一个坑。
	SessionID uint64 `json:"session_id,string"`
}

// UploadService 分片上传会话服务
type UploadService interface {
	// Init 初始化分片上传(建会话 + 预扣配额)
	Init(ctx context.Context, caller *Caller, req MultipartInitReq) (*MultipartInitResp, error)
	// UploadPart 上传一个分片
	UploadPart(ctx context.Context, caller *Caller, req UploadPartReq) (*PartInfo, error)
	// Parts 查已传分片(断点续传依据)
	Parts(ctx context.Context, caller *Caller, sessionID uint64) ([]PartInfo, error)
	// Complete 合并分片 + 建 sys_file 行 + 结算配额
	Complete(ctx context.Context, caller *Caller, req CompleteReq) (*FileView, error)
	// Cancel 取消会话 + 回滚预扣
	Cancel(ctx context.Context, caller *Caller, sessionID uint64) error
	// SupportsMultipart 当前 provider 支持分片吗
	//
	// ⚠️ 暴露出来是为了让 handler 能返 501 而不是 500。local 和 mock 都实现了,
	// aliyun 也实现了,但换成别的 provider 可能没有 —— 前端据此决定要不要
	// 退回单次上传,而不是对着一个 500 弹"上传失败"。
	SupportsMultipart() bool
}
