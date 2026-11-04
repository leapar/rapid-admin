// 自定义时间类型 —— 序列化 + 东八区
package model

import (
	"database/sql/driver"
	"fmt"
	"time"

	"rapid-admin/internal/pkg/timeutil"
)

// Time 业务时间(强制东八区)
type Time struct {
	time.Time
}

// MarshalJSON 序列化为 ISO8601
func (t Time) MarshalJSON() ([]byte, error) {
	if t.Time.IsZero() {
		return []byte("null"), nil
	}
	// 转为东八区
	return []byte(fmt.Sprintf(`"%s"`, t.Time.In(timeutil.AsiaShanghai).Format("2006-01-02T15:04:05+08:00"))), nil
}

// UnmarshalJSON
func (t *Time) UnmarshalJSON(data []byte) error {
	if len(data) == 0 || string(data) == "null" {
		return nil
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04:05Z07:00", string(data[1:len(data)-1]), timeutil.AsiaShanghai)
	if err != nil {
		return err
	}
	t.Time = parsed
	return nil
}

// Scan 实现 sql.Scanner
func (t *Time) Scan(v interface{}) error {
	if v == nil {
		t.Time = time.Time{}
		return nil
	}
	switch val := v.(type) {
	case time.Time:
		t.Time = val
	case []byte:
		parsed, err := time.Parse("2006-01-02 15:04:05", string(val))
		if err != nil {
			return err
		}
		t.Time = parsed
	case string:
		parsed, err := time.Parse("2006-01-02 15:04:05", val)
		if err != nil {
			return err
		}
		t.Time = parsed
	}
	return nil
}

// Value 实现 driver.Valuer
func (t Time) Value() (driver.Value, error) {
	if t.Time.IsZero() {
		return nil, nil
	}
	return t.Time, nil
}

// Now 当前时间(东八区)
func Now() Time {
	return Time{Time: timeutil.Now()}
}

// Ptr 指针形式
func (t Time) Ptr() *Time { return &t }
