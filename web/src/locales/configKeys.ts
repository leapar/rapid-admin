/**
 * 系统配置页的 i18n key 派生规则(2026-10-29)
 *
 * ## 为什么单独一个文件
 *
 *   1. `react-refresh` 规则要求"只导出组件"的模块不能再导出别的东西 ——
 *      纯函数跟组件放一起会触发 lint 报错(本仓 `--max-warnings 0`)。
 *   2. **守门网要 import 它**(`locales/__tests__/config-i18n.test.ts`)——
 *      守的是**页面上真在用的那条规则**,不是抄一份。
 *      抄一份的话,规则改了测试不会红,网就成摆设了。
 *
 * ## 为什么 `.` / `-` 要换成 `_`
 *
 *   `t()` 是**点分路径**查找(`locales/useI18n.ts` 的 `getByPath` 按 `.` split),
 *   直接拿 `aliyun.sms.access_key` 当 key 查不到 ——
 *   `getByPath(dict, 'page.config.item.aliyun.sms.access_key')` 会去找
 *   `dict.page.config.item.aliyun.sms.access_key` 这个**嵌套结构**,
 *   而词条表里是一个平铺的 `aliyun_sms_access_key`。
 *
 *   ⇒ 一律把 `.` / `-` 换成 `_`。
 */
export const CONFIG_ITEM_I18N_PREFIX = 'page.config.item'
export const CONFIG_DICT_I18N_PREFIX = 'page.config.dict'

/** 配置项 → i18n key(例:`aliyun.sms.access_key` → `page.config.item.aliyun_sms_access_key`) */
export function configItemI18nKey(configKey: string): string {
  return `${CONFIG_ITEM_I18N_PREFIX}.${configKey.replace(/[.-]/g, '_')}`
}

/** 字典项 → i18n key(例:`config.value` → `page.config.dict.config_value`) */
export function configDictI18nKey(itemKey: string): string {
  return `${CONFIG_DICT_I18N_PREFIX}.${itemKey.replace(/[.-]/g, '_')}`
}
