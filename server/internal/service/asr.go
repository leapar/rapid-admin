// 语音识别业务层(asrx 的装配点)
//
// ⚠️ **识别与克隆/合成共用同一把百炼凭据** —— 走同一份"生效配置"
// (配置文件打底 + 配置中心覆盖,ResolveVoiceConfig),所以识别也能
// 吃到面板改的 api_key / workspace_id,不用重启。
package service

import (
	"context"
	"io"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/asrx"
)

// AsrService 语音识别:一句话(同步)+ 实时会话(WS 代理用)
type AsrService interface {
	// RecognizeOnce 一句话识别:整段音频 → 聚合后的纯文本。
	// 短音频(<60s)一发一收;长录音不该走这里。
	RecognizeOnce(ctx context.Context, p asrx.RecognizeParams, audio io.Reader) (string, error)
	// Stream 开一次实时识别会话(WS 代理架在它上面):
	// 返回的会话已过 task-started,调用方可直接 Send 音频 / 收 Results。
	Stream(ctx context.Context, p asrx.RecognizeParams) (*asrx.Session, error)
}

type asrService struct {
	cfg     config.VoiceConfig
	resolve func(ctx context.Context) (config.VoiceConfig, error)
}

// NewAsrService 构造。resolve 与克隆/合成侧同一份(见 main 装配)。
func NewAsrService(cfg config.VoiceConfig, resolve func(ctx context.Context) (config.VoiceConfig, error)) AsrService {
	return &asrService{cfg: cfg, resolve: resolve}
}

func (s *asrService) client(ctx context.Context) (*asrx.Client, error) {
	cfg := effectiveVoiceCfg(ctx, s.cfg, s.resolve)
	return asrx.New(asrx.Config{
		APIKey:         cfg.APIKey,
		WorkspaceID:    cfg.WorkspaceID,
		Model:          cfg.AsrModel,
		TimeoutSeconds: cfg.AsrTimeoutSeconds,
	})
}

func (s *asrService) RecognizeOnce(ctx context.Context, p asrx.RecognizeParams, audio io.Reader) (string, error) {
	client, err := s.client(ctx)
	if err != nil {
		return "", err
	}
	return client.Once(ctx, p, audio)
}

func (s *asrService) Stream(ctx context.Context, p asrx.RecognizeParams) (*asrx.Session, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	return client.Stream(ctx, p)
}
