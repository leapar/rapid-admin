/**
 * useDict —— 字典 hook
 *
 * 用法:
 *   const { options, getLabel, loading } = useDict('user_status')
 *   <Select options={options} />
 *   <span>{getLabel(1)}</span>
 *   <Tag color={getColor('active')}>{getLabel('active')}</Tag>
 *
 * 字典约束(2026-10-18 L3):
 *   - groupCode 必须是 DictGroupCode 联合类型成员(自动补全 + 拼写检查)
 *   - 镜像 server/internal/seed/seed.go mustDicts
 *   - 若新 enum 不在联合类型,TS 编译直接报错,提示加到 web/src/types/dict.ts
 *
 * 颜色约定:
 *   - 字典 item 的 description 字段可写 'color:green' / 'color:red' 形式(临时方案)
 *   - 业务页用 getColor(value) 取 antd Tag color
 *   - P1:把 color 提到字典表的独立列(见 docs/数据字典清单.md)
 *
 * ⚠️ **`is_active` 的过滤只发生在 `options` 上**(2026-10-28 M107):
 *   - `options`(下拉选项)= **只给启用项** —— 这是「临时下线某个选项」的实现,
 *     `docs/数据字典管理规范.md` 规则 2 早就这么承诺了(以前是**空头承诺**:
 *     useDict 不过滤、后端 ListDict 也不过滤 ⇒ 运营把一项「禁用」之后,
 *     所有业务页的下拉里它还在 ⇒ 那个开关是**死的**)。
 *   - `getLabel` / `getColor` / `raw` = **仍然能取到禁用项** ——
 *     历史数据里已经存了这个 value,表格 / 详情要照常显示中文标签和颜色;
 *     全局滤掉的话,老记录会变成一串裸 value("closed" 而不是"已关闭")。
 *   - 语义:**下线 = 不再提供新选择,既有数据照常可读。**
 */
import { useEffect, useMemo, useState } from 'react'
import { useDictStore } from '@/stores/dict'
import type { DictItem } from '@/api/config'
import type { DictGroupCode } from '@/types/dict'

export type TagColor = 'green' | 'red' | 'orange' | 'blue' | 'purple' | 'cyan' | 'gold' | 'default'

export interface UseDictResult {
  options: { value: string | number; label: string; raw: DictItem }[]
  getLabel: (value: string | number) => string
  /** 从字典项 description 'color:xxx' 解析 antd Tag color,缺省 'default' */
  getColor: (value: string | number) => TagColor
  loading: boolean
  /**
   * 拉取失败(拦截器已弹过提示)—— 用来区分「还在加载」/「拉到了但这一组是空的」
   * /「拉失败了」。⚠️ 以前失败被 `.catch(() => undefined)` 吞掉,
   *   `loading` 永远是 true ⇒ 下拉永远转圈(同 M106 CaptchaField 那一类)。
   */
  error: boolean
  raw: DictItem[]
}

// 从 description 字符串里解析 'color:xxx'
// 例子: 'color:green # 用户状态-正常' → 'green'
function parseColor(desc: string | undefined): TagColor {
  if (!desc) return 'default'
  const m = desc.match(/^color:(\w+)/)
  if (!m) return 'default'
  const c = m[1] as TagColor
  if (['green', 'red', 'orange', 'blue', 'purple', 'cyan', 'gold', 'default'].includes(c)) {
    return c
  }
  return 'default'
}

export function useDict(groupCode: DictGroupCode): UseDictResult {
  const fetchDict = useDictStore((s) => s.fetchDict)
  const dicts = useDictStore((s) => s.dicts[groupCode])
  const lastFetchAt = useDictStore((s) => s.lastFetchAt[groupCode])
  const getLabel = useDictStore((s) => s.getLabel)

  // ⚠️ 拉失败必须能落到 UI 上 —— 否则 `loading: !dicts` 永远为 true,
  //   下拉永远转圈,用户看不出是"没数据"还是"拉挂了"(M106 在 CaptchaField 上踩过同一个坑)。
  const [failed, setFailed] = useState(false)

  useEffect(() => {
    // 已有新鲜数据 → 不用再拉
    if (dicts && lastFetchAt) return
    let cancelled = false
    setFailed(false)
    fetchDict(groupCode).catch(() => {
      // ⚠️ 组件已经卸载 / 已经切到别的组 → 不要再 setState
      if (!cancelled) setFailed(true)
    })
    return () => {
      cancelled = true
    }
  }, [groupCode, dicts, lastFetchAt, fetchDict])

  const options = useMemo(() => {
    return (
      (dicts || [])
        // ⚠️ 只在这里过滤 `is_active`(理由见文件头)。
        //   `!== false` 而不是 `=== true`:字段缺失(undefined)时**保留**该项,
        //   宁可多显示一个选项,也不要把整组下拉清空。
        .filter((d) => d.is_active !== false)
        .map((d) => ({
          value: d.item_value,
          label: d.label,
          raw: d,
        }))
    )
  }, [dicts])

  const getColor = (v: string | number): TagColor => {
    const list = dicts || []
    const item = list.find((d) => String(d.item_value) === String(v))
    return parseColor(item?.description)
  }

  return {
    options,
    getLabel: (v) => getLabel(groupCode, v),
    getColor,
    loading: !dicts && !failed,
    error: failed,
    raw: dicts || [],
  }
}
