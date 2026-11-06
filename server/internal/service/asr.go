// 语音识别业务层(asrx 的装配点)
//
// ⚠️ **识别与克隆/合成共用同一把百炼凭据** —— 走同一份"生效配置"
// (配置文件打底 + 配置中心覆盖,ResolveVoiceConfig),所以识别也能
// 吃到面板改的 api_key / workspace_id,不用重启。
package service

import (
	"context"
	"io"

	"go.uber.org/zap"

	"rapid-admin/internal/config"
	"rapid-admin/internal/pkg/asrx"
	"rapid-admin/internal/pkg/logger"
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
	text, err := client.Once(ctx, p, audio)
	if err != nil {
		// 失败原因落一份应用日志:5xx 出去后系统日志(sys_system_log)只记
		// "接口 5xx: POST /biz/asr/recognize status=500",凭据没配 /
		// task-failed 的具体原因只在响应体里,翻日志查不到
		logger.L().Warn("asr: 一句话识别失败", zap.Error(err))
		return "", err
	}
	return text, nil
}

func (s *asrService) Stream(ctx context.Context, p asrx.RecognizeParams) (*asrx.Session, error) {
	client, err := s.client(ctx)
	if err != nil {
		return nil, err
	}
	sess, err := client.Stream(ctx, p)
	if err != nil {
		logger.L().Warn("asr: 实时识别建会话失败", zap.Error(err))
		return nil, err
	}
	return sess, nil
}
