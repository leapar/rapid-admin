package dynx

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 文件字段
// ============================================================================

// TestNormalize_文件字段拒带域名的值 ——
// 字段存的是"不带域名的路径"(设计文档 §2.9)。
// 存进 https://cdn.x.com/... 会让"换域名 / 换 CDN"变成一次数据迁移。
func TestNormalize_文件字段拒带域名的值(t *testing.T) {
	_, err := Normalize(TypeFile, "https://cdn.example.com/rapid-admin/1/dyn/0/a.pdf", Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "域名")
}

// TestNormalize_文件字段拒危险路径 ——
// key 是要拼进静态路由与对象存储路径的,`..` 能爬出目录。
func TestNormalize_文件字段拒危险路径(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"含 ..", "rapid-admin/../etc/passwd", ".."},
		{"含反斜杠", `C:\temp\a.pdf`, "反斜杠"},
		{"前导斜杠", "/rapid-admin/1/a.pdf", "相对路径"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Normalize(TypeFile, tc.in, Options{})
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestNormalize_文件字段接受合法的 key ——
// key 的形状是「路径根/uid/biz/fid/对象名」,不带域名、不带挂载点。
func TestNormalize_文件字段接受合法的key(t *testing.T) {
	got, err := Normalize(TypeFile, "  rapid-admin/1844/dyn/0/1712345678-a1b2-户型图.pdf  ", Options{})
	require.NoError(t, err)
	assert.Equal(t, "rapid-admin/1844/dyn/0/1712345678-a1b2-户型图.pdf", got)
}

// TestNormalize_文件字段空值是"没填" ——
// 空串与 null 都不是错:选填的附件字段不填是正常用法。
func TestNormalize_文件字段空值是没填(t *testing.T) {
	got, err := Normalize(TypeFile, "", Options{})
	require.NoError(t, err)
	assert.Equal(t, "", got)

	got, err = Normalize(TypeFile, nil, Options{})
	require.NoError(t, err)
	assert.Nil(t, got)
}

// TestNormalize_文件字段长度上限 ——
// 上限必须与 sys_file.file_key 的列宽(512)对齐。
// 短一截是"存不下",长一截是"截断后查不到文件"。
func TestNormalize_文件字段长度上限(t *testing.T) {
	long := strings.Repeat("a", maxFilePathBytes+1)
	_, err := Normalize(TypeFile, long, Options{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "过长")

	// 正好 512 字节要通过
	ok := strings.Repeat("a", maxFilePathBytes)
	got, err := Normalize(TypeFile, ok, Options{})
	require.NoError(t, err)
	assert.Equal(t, ok, got)
}

// Test往返_文件字段 ——
// 归一化后再反归一化,值必须一模一样(附件的 key 不能被改写)。
func Test往返_文件字段(t *testing.T) {
	key := "rapid-admin/1844/dyn/0/1712345678-a1b2-户型图.pdf"
	v, err := Normalize(TypeFile, key, Options{})
	require.NoError(t, err)
	got := Denormalize(TypeFile, v)
	assert.Equal(t, key, got)
}

// ============================================================================
// 选项来源
// ============================================================================

// TestMarshalOptions_有source时不能被静默丢掉 ——
// 以前这里是 `if len(o.Choices) == 0 { return "", nil }`,
// 那样"关联表来源"的配置会被静默丢掉:字段建好了,却变成一个
// 没有任何选项的单选 —— 而前端不报错。
func TestMarshalOptions_有source时不能被静默丢掉(t *testing.T) {
	raw, err := MarshalOptions(Options{Source: &Source{Kind: SourceKindTable, Key: "user"}})
	require.NoError(t, err)
	require.NotEmpty(t, raw, "关联表来源的配置不能被序列化成空串")

	got, err := ParseOptions(raw)
	require.NoError(t, err)
	assert.True(t, got.HasSource())
	assert.Equal(t, "user", got.Source.Key)
	assert.Equal(t, SourceKindTable, got.Source.Kind)
}

// TestMarshalOptions_空选项集仍然返回空串 ——
// 一个还没配选项的 select 字段,存的就是空串。
func TestMarshalOptions_空选项集仍然返回空串(t *testing.T) {
	raw, err := MarshalOptions(Options{})
	require.NoError(t, err)
	assert.Equal(t, "", raw)
}

// TestOptions_来源与手工选项互不影响 ——
// 关联表来源的字段,手工选项集是空的 ⇒ 写入时**不做成员资格校验**
// (表里的行会被删,校验成员资格会让历史记录再也存不进去任何修改)。
func TestOptions_来源与手工选项互不影响(t *testing.T) {
	o := Options{Source: &Source{Kind: SourceKindTable, Key: "user"}}
	assert.False(t, o.HasChoices(), "关联表来源没有手工选项")
	assert.False(t, o.Contains("123"), "没有手工选项时不该有成员资格这回事")
}

// TestValidate_来源的kind与key ——
// kind 只能是 table,key 不能为空。
func TestValidate_来源的kind与key(t *testing.T) {
	err := Options{Source: &Source{Kind: "sql", Key: "user"}}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "来源种类")

	err = Options{Source: &Source{Kind: SourceKindTable, Key: "  "}}.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "key 不能为空")
}

// TestValidateSourceKey_注册表是白名单 ——
// 让用户填表名等于把 SQL 的表名交给用户拼 ⇒ 那是注入面。
func TestValidateSourceKey_注册表是白名单(t *testing.T) {
	require.NoError(t, ValidateSourceKey("user"))
	require.NoError(t, ValidateSourceKey("role"))
	require.NoError(t, ValidateSourceKey("dept"))

	err := ValidateSourceKey("sys_user; DROP TABLE x")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "未知的选项来源")

	err = ValidateSourceKey("")
	require.Error(t, err)
}

// TestParseOptions_来源被删时不解析失败 ——
// 注册表是会变的(加表 / 删表)⇒ 一个老字段的来源被删了,
// 读的时候不该整个选项集解析失败(那会让那个字段的选项悄无声息地消失)。
// ⇒ "key 是不是合法的来源"在**设计期**查(ValidateSourceKey),不在读的时候查。
func TestParseOptions_来源被删时不解析失败(t *testing.T) {
	raw := `{"choices":[],"source":{"kind":"table","key":"gone_table"}}`
	got, err := ParseOptions(raw)
	require.NoError(t, err)
	assert.True(t, got.HasSource())
	assert.Equal(t, "gone_table", got.Source.Key)
}

// ============================================================================
// 来源注册表
// ============================================================================

// TestRelationSources_每个来源都要带齐判定所需的东西 ——
// 少一个字段 = 那一层的闸门失效(判不了权限 / 拼不出 SQL)。
func TestRelationSources_每个来源都要带齐判定所需的东西(t *testing.T) {
	sources := RelationSources()
	require.NotEmpty(t, sources)
	for _, s := range sources {
		assert.NotEmpty(t, s.Key, "来源缺 key")
		assert.NotEmpty(t, s.Label, "来源 %q 缺中文名", s.Key)
		assert.NotEmpty(t, s.Table, "来源 %q 缺表名", s.Key)
		assert.NotEmpty(t, s.ValueColumn, "来源 %q 缺取值列", s.Key)
		assert.NotEmpty(t, s.LabelSQL, "来源 %q 缺显示名列", s.Key)
		assert.NotEmpty(t, s.Module, "来源 %q 缺权限模块", s.Key)
		assert.NotEmpty(t, s.Action, "来源 %q 缺权限动作", s.Key)
		assert.NotEmpty(t, s.ResourceCode, "来源 %q 缺资源码", s.Key)
		// 资源码必须与模块:动作对得上(前端提示"缺哪个权限"靠它)
		assert.Equal(t, s.Module+":"+s.Action, s.ResourceCode,
			"来源 %q 的资源码与模块/动作对不上", s.Key)
	}
}

// TestRelationSources_表名是白名单里的系统表 ——
// 拼进 SQL 的表名必须来自注册表,不能来自用户输入。
func TestRelationSources_表名是白名单里的系统表(t *testing.T) {
	allowed := map[string]bool{
		"sys_user": true, "sys_role": true, "sys_department": true,
	}
	for _, s := range RelationSources() {
		assert.True(t, allowed[s.Table], "来源 %q 的表 %q 不在白名单里", s.Key, s.Table)
	}
}

// TestLookupRelationSource_查不到就是查不到 ——
// 老配置指向的来源被删了 ⇒ 查不到(而不是返回一个空壳)。
func TestLookupRelationSource_查不到就是查不到(t *testing.T) {
	got, ok := LookupRelationSource("user")
	require.True(t, ok)
	assert.Equal(t, "sys_user", got.Table)

	_, ok = LookupRelationSource("nope")
	assert.False(t, ok)

	// 前后空白要能trim(前端传参可能带空格)
	_, ok = LookupRelationSource("  user  ")
	assert.True(t, ok)
}

// TestRelationSourceKeys_与注册表一致 ——
// 报错信息里列出的可选来源必须是真的可选的那些。
func TestRelationSourceKeys_与注册表一致(t *testing.T) {
	keys := RelationSourceKeys()
	require.Len(t, keys, len(RelationSources()))
	for _, k := range keys {
		_, ok := LookupRelationSource(k)
		assert.True(t, ok, "key %q 在注册表里查不到", k)
	}
}
