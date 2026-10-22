/**
 * localStorage 键名一次性迁移。
 *
 * 背景：commit 2184622「全仓改名 Nexus→CortexNEL」漏改了前端的三枚
 * localStorage 键（theme / group.dismissed / setup.dismissed），它们仍以
 * `nexus.` 开头。直接改键名会丢用户偏好（主题重置、已关闭的弹窗重新弹出），
 * 因此这里做「读旧键 → 写新键 → 删旧键」的无感迁移。
 *
 * 幂等：新键已存在时不覆盖（以新键为准），旧键无论有无值都删除；
 * 重复调用无副作用。启动时调用一次即可（见 main.ts）。
 */

/** [旧键, 新键]。新增键时在此登记，不要在使用处各写一遍字面量。 */
const KEY_RENAMES: ReadonlyArray<readonly [string, string]> = [
  ['nexus.theme', 'cortexnel.theme'],
  ['nexus.group.dismissed', 'cortexnel.group.dismissed'],
  ['nexus.setup.dismissed', 'cortexnel.setup.dismissed'],
]

/** 执行遗留键名迁移（幂等，可在每次启动时调用）。 */
export function migrateLegacyStorageKeys(): void {
  for (const [from, to] of KEY_RENAMES) {
    const legacy = localStorage.getItem(from)
    if (legacy !== null && localStorage.getItem(to) === null) {
      localStorage.setItem(to, legacy)
    }
    localStorage.removeItem(from)
  }
}
