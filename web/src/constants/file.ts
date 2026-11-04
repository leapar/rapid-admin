/**
 * 文件相关的枚举常量 —— 对齐后端 `model.FileStorage`
 *
 * ⚠️ **为什么前端要有一份**:后端的枚举是 int(1 / 2),直接透传到页面。
 *   页面必须知道每个数字对应哪个存储后端,否则只能靠"猜"。
 *
 * ⚠️ **这里的取值必须与后端一致**(server/internal/model/file.go):
 *   ```go
 *   FileStorageLocal FileStorage = 1 // 本地磁盘
 *   FileStorageOSS   FileStorage = 2 // 阿里云 OSS
 *   ```
 *   早先页面里写的是 `v === 1 ? 'OSS' : '本地'` —— **两个值搞反了**:
 *   本地磁盘的文件被显示成 "OSS",真躺在 OSS 上的文件反而显示 "本地"。
 *   而 `sys_file.storage` 是**逐行**记的(迁移期两种 provider 共存),
 *   显示错了就等于这个字段在页面上完全没用。
 *   后端 model/file_test.go 的 TestEnumZeroValues 锁着这两个值,
 *   前端这边由 pages/file/__tests__/list.test.tsx 锁。
 */
export const FileStorage = {
  /** 本地磁盘 */
  LOCAL: 1,
  /** 阿里云 OSS */
  OSS: 2,
} as const

export type FileStorageValue = (typeof FileStorage)[keyof typeof FileStorage]

/**
 * 是不是 OSS 存储
 *
 * ⚠️ **只认 2**。历史上出现过"把 1 当 OSS"的写法,于是本地文件全被标成
 *   OSS。这里显式收窄:只有恰好等于 FileStorage.OSS 才算 OSS,
 *   其它值(0 / 未知)一律当"非 OSS"处理,不猜。
 */
export function isOSSStorage(v: number | undefined | null): boolean {
  return v === FileStorage.OSS
}

/**
 * 这个 content_type 能不能当**文件夹封面**
 *
 * ⚠️ **必须与后端同口径** —— 后端 `service/folder_impl.go` 的 `isCoverImage`:
 *   · 必须是 `image/` 开头;
 *   · **排除 SVG**(SVG 里能塞 `<script>`,而封面会被"点开看大图"打开成
 *     同源文档 —— 那是存储变成站内 XSS 投放位)。
 *
 *   前端要是放行 SVG,用户点了「设为封面」后端会返 400,
 *   表现是"按钮能点、点了报错" —— 而用户完全不知道是自己选错了文件。
 *   口径分叉的代价就是这种"前端说行、后端说不行"。
 */
export function canBeCover(contentType: string | undefined | null): boolean {
  const ct = (contentType || '').toLowerCase().trim()
  if (!ct.startsWith('image/')) return false
  if (ct.includes('svg')) return false
  return true
}
