/**
 * frontend/src/lib/storageMigration.ts 的回归验证。
 *
 * 自包含：用 esbuild API 就地转译被测模块（TypeScript → ESM）到临时文件，
 * 动态 import 后执行断言，最后清理临时文件。无需预先构建。
 *
 * 运行：npm run test:storage-migration   （或 node storageMigration.check.mjs）
 *
 * 覆盖：正常迁移 / 新键已存在时不覆盖 / 幂等 / 无旧键时无害 / 空串值也迁移。
 */
import { readFile, writeFile, unlink } from 'node:fs/promises'
import { fileURLToPath } from 'node:url'
import { dirname, join } from 'node:path'
import { transform } from 'esbuild'

const here = dirname(fileURLToPath(import.meta.url))
const SOURCE = join(here, 'src/lib/storageMigration.ts')
const TEMP = join(here, 'storageMigration.tmp.mjs')

let pass = 0
let fail = 0
function check(name, cond) {
  if (cond) { pass++; console.log('  ok   ' + name) }
  else { fail++; console.log('  FAIL ' + name) }
}

/** 造一个内存 localStorage（Node 无 DOM，注入到 globalThis 供被测模块读取）。 */
function freshStorage(seed) {
  const map = new Map(Object.entries(seed ?? {}))
  return {
    getItem: (k) => (map.has(k) ? map.get(k) : null),
    setItem: (k, v) => map.set(k, String(v)),
    removeItem: (k) => map.delete(k),
    _map: map,
  }
}

const source = await readFile(SOURCE, 'utf8')
const compiled = await transform(source, { format: 'esm', loader: 'ts' })
await writeFile(TEMP, compiled.code, 'utf8')

let migrateLegacyStorageKeys
try {
  ;({ migrateLegacyStorageKeys } = await import(`file://${TEMP.replace(/\\/g, '/')}`))
} finally {
  await unlink(TEMP).catch(() => {})
}

console.log('case 1: 旧键全部有值 → 迁到新键并删旧键')
{
  globalThis.localStorage = freshStorage({
    'nexus.theme': 'dark',
    'nexus.group.dismissed': '1',
    'nexus.setup.dismissed': '1',
  })
  migrateLegacyStorageKeys()
  const s = globalThis.localStorage
  check('theme 迁到 cortexnel.theme=dark', s.getItem('cortexnel.theme') === 'dark')
  check('group 迁到 cortexnel.group.dismissed=1', s.getItem('cortexnel.group.dismissed') === '1')
  check('setup 迁到 cortexnel.setup.dismissed=1', s.getItem('cortexnel.setup.dismissed') === '1')
  check('旧 nexus.theme 已删除', s.getItem('nexus.theme') === null)
  check('旧 nexus.group.dismissed 已删除', s.getItem('nexus.group.dismissed') === null)
  check('旧 nexus.setup.dismissed 已删除', s.getItem('nexus.setup.dismissed') === null)
}

console.log('case 2: 新键已存在 → 以新键为准，旧键仍删除')
{
  globalThis.localStorage = freshStorage({ 'nexus.theme': 'dark', 'cortexnel.theme': 'light' })
  migrateLegacyStorageKeys()
  check('新键值不被旧键覆盖（仍是 light）', globalThis.localStorage.getItem('cortexnel.theme') === 'light')
  check('旧键已删除', globalThis.localStorage.getItem('nexus.theme') === null)
}

console.log('case 3: 幂等 —— 连跑两次结果一致')
{
  globalThis.localStorage = freshStorage({ 'nexus.theme': 'dark' })
  migrateLegacyStorageKeys()
  const after1 = new Map(globalThis.localStorage._map)
  migrateLegacyStorageKeys()
  const after2 = globalThis.localStorage._map
  check('两次后键集合相同', after1.size === after2.size && [...after1.keys()].every((k) => after2.has(k)))
  check('值仍为 dark', globalThis.localStorage.getItem('cortexnel.theme') === 'dark')
}

console.log('case 4: 没有任何旧键 → 不报错、不产生新键')
{
  globalThis.localStorage = freshStorage({ 'other.key': 'x' })
  migrateLegacyStorageKeys()
  const s = globalThis.localStorage
  check('未凭空创建新键', s.getItem('cortexnel.theme') === null &&
    s.getItem('cortexnel.group.dismissed') === null &&
    s.getItem('cortexnel.setup.dismissed') === null)
  check('无关键不受影响', s.getItem('other.key') === 'x')
}

console.log('case 5: 空串值也迁移（getItem 返回 "" 不是 null）')
{
  globalThis.localStorage = freshStorage({ 'nexus.theme': '' })
  migrateLegacyStorageKeys()
  check('空串迁到新键', globalThis.localStorage.getItem('cortexnel.theme') === '')
  check('旧键删除', globalThis.localStorage.getItem('nexus.theme') === null)
}

console.log(`\n结果: ${pass} 通过, ${fail} 失败`)
process.exit(fail === 0 ? 0 : 1)
