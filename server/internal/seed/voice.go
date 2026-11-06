// 内置 TTS 音色 seed —— 百炼 CosyVoice + Edge 朗读的出厂音色登记
//
// ⚠️ **只补缺,不覆盖**:按 (tts_type, voice_id) 判存在 —— 运营在登记册里
// 改过名/排序/推荐位的行,重跑 seed 不回滚;删掉的行也不复活(软删行
// 查重时会查到,不会重复插入)。要重新内置某音色,把软删行恢复即可。
package seed

import (
	"fmt"

	"gorm.io/gorm"

	"rapid-admin/internal/model"
	"rapid-admin/internal/pkg/sonyflake"
	"rapid-admin/internal/pkg/voicex"
)

// SeedBuiltinTtsVoices 内置音色登记(挂 seed.Run 末尾,幂等)
func SeedBuiltinTtsVoices(db *gorm.DB) error {
	voices := voicex.BuiltinTtsVoices()
	inserted := 0
	if err := db.Transaction(func(tx *gorm.DB) error {
		for _, v := range voices {
			// 软删行也算"存在"—— 查重走默认 scope 之外的 Unscoped,
			// 否则"删了内置音色再跑 migrate"会插回一份重复行
			var n int64
			if err := tx.Unscoped().Model(&model.SysTtsVoice{}).
				Where("tts_type = ? AND voice_id = ?", v.TtsType, v.VoiceID).
				Count(&n).Error; err != nil {
				return fmt.Errorf("query builtin tts voice %s: %w", v.VoiceID, err)
			}
			if n > 0 {
				continue
			}
			if err := tx.Create(&model.SysTtsVoice{
				BaseModel:   model.BaseModel{ID: sonyflake.Next()},
				Name:        v.Name,
				VoiceID:     v.VoiceID,
				Gender:      v.Gender,
				Sex:         v.Sex,
				TtsType:     v.TtsType,
				Sort:        10,
				Description: v.Description,
				Recommend:   v.Recommend,
			}).Error; err != nil {
				return fmt.Errorf("create builtin tts voice %s: %w", v.VoiceID, err)
			}
			inserted++
		}
		return nil
	}); err != nil {
		return err
	}
	if inserted > 0 {
		fmt.Println("[seed] builtin tts voices inserted:", inserted, "(bailian cosyvoice + edge)")
	}
	return nil
}
