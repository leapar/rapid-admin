// CaptchaService 图形验证码
// 文档 §9.2 — 登录防爆破
package service

import (
	"context"
	"sync"
	"time"

	"github.com/mojocn/base64Captcha"
)

// captchaStore 进程内 captcha 存储(替代 Redis,dev 够用)
//   - key:  captcha id
//   - value: 正确答案
//   - 过期: 5 min
type captchaStore struct {
	mu    sync.Mutex
	data  map[string]captchaItem
	life  time.Duration
	clock func() time.Time
}

type captchaItem struct {
	code    string
	expires time.Time
}

func newCaptchaStore() *captchaStore {
	return &captchaStore{
		data:  make(map[string]captchaItem),
		life:  5 * time.Minute,
		clock: time.Now,
	}
}

func (s *captchaStore) Set(id string, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[id] = captchaItem{code: value, expires: s.clock().Add(s.life)}
	return nil
}

func (s *captchaStore) Get(id string, clear bool) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	it, ok := s.data[id]
	if !ok || s.clock().After(it.expires) {
		delete(s.data, id)
		return ""
	}
	if clear {
		delete(s.data, id)
	}
	return it.code
}

func (s *captchaStore) Verify(id, answer string, clear bool) bool {
	expected := s.Get(id, clear)
	return expected != "" && expected == answer
}

// ===== service interface =====

// CaptchaGenerateResult 生成结果
type CaptchaGenerateResult struct {
	ID         string `json:"id"`
	Base64Data string `json:"image"` // data:image/png;base64,...
	Type       string `json:"type"`  // image/png
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	Length     int    `json:"length"`
	ExpiresIn  int    `json:"expires_in"` // 秒
}

// CaptchaService 图形验证码
type CaptchaService interface {
	Generate(ctx context.Context) (*CaptchaGenerateResult, error)
	Verify(ctx context.Context, id, code string) bool
}

// ===== impl =====

type captchaService struct {
	store *captchaStore
}

func NewCaptchaService() CaptchaService {
	return &captchaService{store: newCaptchaStore()}
}

// Generate 生成 4 位数字图形验证码
func (s *captchaService) Generate(_ context.Context) (*CaptchaGenerateResult, error) {
	driver := base64Captcha.NewDriverDigit(80, 240, 4, 0.7, 60)
	c := base64Captcha.NewCaptcha(driver, s.store)
	id, b64, answer, err := c.Generate()
	if err != nil {
		return nil, err
	}
	return &CaptchaGenerateResult{
		ID:         id,
		Base64Data: b64,
		Type:       "image/png",
		Width:      240,
		Height:     80,
		Length:     len(answer),
		ExpiresIn:  int(s.store.life.Seconds()),
	}, nil
}

// Verify 验证 captcha(成功即清除)
func (s *captchaService) Verify(_ context.Context, id, code string) bool {
	return s.store.Verify(id, code, true)
}
