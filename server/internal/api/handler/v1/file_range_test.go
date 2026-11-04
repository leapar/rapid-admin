// GET /files/:id/content 的 Range 处理 —— M80 收尾
//
// ⚠️ 这里守的是**响应头算得对不对**,不是"能不能下完文件" —— 那个在
// TestContent_PrivateFileStreamsBytes 里。
//
// 为什么必须测:`<video>` 靠 206 + Content-Range 才知道"这段是哪一段"。
// Content-Range 算错一个字节,客户端就会把两段拼成一个坏掉的文件,而且
// **画面能出来、声音也在**,只有拖到某一处才花屏 —— 这种 bug 上生产之后
// 几乎不可能靠日志定位。
//
// 不需要起服务:parseRange 是纯函数。
package v1

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// size = 100 的文件,和下面的期望值一一对应
func TestParseRange_CommonForms(t *testing.T) {
	cases := []struct {
		hdr        string
		size       int64
		wantSt     rangeResult
		wantStart  int64
		wantLength int64
		why        string
	}{
		// ---- 忽略:整份 200 ----
		{"", 100, rangeFull, 0, 0, "没传 Range"},
		{"bytes=0-99", 100, rangePartial, 0, 100, "刚好整份 —— 仍按 206 回(合法)"},
		{"items=0-10", 100, rangeFull, 0, 0, "未知单位必须忽略而不是 416"},
		{"bytes=abc", 100, rangeFull, 0, 0, "不是数字"},
		{"bytes=9-3", 100, rangeFull, 0, 0, "区间倒挂是**非法**不是越界"},
		{"bytes=0-1,5-6", 100, rangeFull, 0, 0, "多段 → 整份(RFC 允许)"},
		{"bytes=-", 100, rangeFull, 0, 0, "两头都空"},
		{"bytes=0-99", 0, rangeFull, 0, 0, "大小未知时绝不猜范围"},

		// ---- 正常区间 ----
		{"bytes=0-49", 100, rangePartial, 0, 50, "开头"},
		{"bytes=10-19", 100, rangePartial, 10, 10, "中间"},
		{"bytes=90-", 100, rangePartial, 90, 10, "开区间"},
		{"BYTES=0-9", 100, rangePartial, 0, 10, "单位大小写不敏感"},
		{"bytes= 0-9", 100, rangeFull, 0, 0, "RFC 的文法里 first-byte-pos 前没有 OWS,带空格即非法 → 忽略"},
		{"bytes=-10", 100, rangePartial, 90, 10, "后缀形态:最后 10 字节"},
		{"bytes=-500", 100, rangePartial, 0, 100, "后缀超长 → 夹成整份"},
		{"bytes=0-1000", 100, rangePartial, 0, 100, "终点越界 → 夹到末尾"},

		// ---- 416:起点就在文件外 ----
		{"bytes=100-", 100, rangeUnsatisfiable, 0, 0, "起点 == size"},
		{"bytes=500-600", 100, rangeUnsatisfiable, 0, 0, "起点远超文件"},
		{"bytes=-0", 100, rangeUnsatisfiable, 0, 0, "后缀长度 0 没有意义"},
	}
	for _, tc := range cases {
		st, start, length := parseRange(tc.hdr, tc.size)
		require.Equalf(t, tc.wantSt, st, "hdr=%q size=%d —— %s", tc.hdr, tc.size, tc.why)
		if tc.wantSt == rangePartial {
			require.Equalf(t, tc.wantStart, start, "hdr=%q —— %s", tc.hdr, tc.why)
			require.Equalf(t, tc.wantLength, length, "hdr=%q —— %s", tc.hdr, tc.why)
			// 区间永远不能越过文件末尾:这一条错了就是坏文件而不是小文件
			require.LessOrEqualf(t, start+length, tc.size,
				"hdr=%q 算出的区间越过了文件末尾", tc.hdr)
			require.Positivef(t, length, "hdr=%q 算出了零长度区间", tc.hdr)
		}
	}
}

// 特征:**Content-Range 里的每个数字都必须对得上**,而这个头是拼出来的字符串,
// 拼错一位(少个空格、斜杠写成反斜杠)编译期发现不了、运行期也不报,只有
// 播放器会安静地少播一段。
//
// ⚠️ 这里调的是 **handler 真正在用的那两个函数**(不是测试里复刻一遍的拼法 ——
// 复刻的那种测试改坏了生产代码也照样绿,是最没用的一类)。
func TestParseRange_ContentRangeString(t *testing.T) {
	st, start, length := parseRange("bytes=100-199", 1000)
	require.Equal(t, rangePartial, st)
	require.Equal(t, "bytes 100-199/1000", contentRangeHeader(start, length, 1000))
	require.Equal(t, "bytes */1000", contentRangeUnsatHeader(1000))

	// 边界:最后一段的末位不能少 1(少一位就是少给一个字节,而 Content-Range
	// 说 199 实际发到 198,客户端永远等不满)
	st, start, length = parseRange("bytes=900-", 1000)
	require.Equal(t, rangePartial, st)
	require.Equal(t, "bytes 900-999/1000", contentRangeHeader(start, length, 1000))

	// 后缀形态的头
	st, start, length = parseRange("bytes=-100", 1000)
	require.Equal(t, rangePartial, st)
	require.Equal(t, "bytes 900-999/1000", contentRangeHeader(start, length, 1000))
}
