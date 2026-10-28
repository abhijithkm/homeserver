// vite is configured with emptyOutDir:false; this keeps dist/.gitkeep and
// clears stale hashed assets from earlier builds.
import { readdirSync, rmSync, writeFileSync, readFileSync, existsSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const dist = join(dirname(fileURLToPath(import.meta.url)), '..', 'dist')
const html = readFileSync(join(dist, 'index.html'), 'utf8')
const assetsDir = join(dist, 'assets')
if (existsSync(assetsDir)) {
  for (const f of readdirSync(assetsDir)) {
    if (!html.includes('/assets/' + f)) rmSync(join(assetsDir, f), { force: true })
  }
}
writeFileSync(join(dist, '.gitkeep'), '')
console.log('postbuild: dist/.gitkeep kept, stale assets cleaned')
