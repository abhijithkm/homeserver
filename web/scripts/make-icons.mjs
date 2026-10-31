// Draws the HomeServer mark (a drive slot with a status light) into the PNG icons the web app
// manifest and iOS home-screen install need. Pure Node (zlib only); run by hand when the mark changes:
//   node scripts/make-icons.mjs
// Output goes to web/public and is committed, so a normal build needs no image tooling.
import { deflateSync } from 'node:zlib'
import { mkdirSync, writeFileSync } from 'node:fs'
import { join, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const out = join(dirname(fileURLToPath(import.meta.url)), '..', 'public')
mkdirSync(out, { recursive: true })

const hex = (h) => [parseInt(h.slice(1, 3), 16), parseInt(h.slice(3, 5), 16), parseInt(h.slice(5, 7), 16)]
const TOP = hex('#1f4249')
const BOTTOM = hex('#12292e')
const SLOT = hex('#091619')
const LED = hex('#1fcf86')

function inRoundRect(x, y, x0, y0, x1, y1, r) {
  if (x < x0 || x > x1 || y < y0 || y > y1) return false
  const cx = x < x0 + r ? x0 + r : x > x1 - r ? x1 - r : x
  const cy = y < y0 + r ? y0 + r : y > y1 - r ? y1 - r : y
  return (x - cx) ** 2 + (y - cy) ** 2 <= r * r
}

/** Colour at a point in unit square coordinates, as [r, g, b, a] with a in 0..1. */
function shade(x, y, full, scale) {
  let rgb = null
  let a = 0
  if (full ? true : inRoundRect(x, y, 0, 0, 1, 1, 0.22)) {
    const t = Math.min(1, Math.max(0, y))
    rgb = TOP.map((c, i) => c + (BOTTOM[i] - c) * t)
    a = 1
  }
  if (!rgb) return [0, 0, 0, 0]
  const u = (x - 0.5) / scale + 0.5
  const v = (y - 0.5) / scale + 0.5
  if (inRoundRect(u, v, 0.19, 0.26, 0.81, 0.38, 0.06) || inRoundRect(u, v, 0.19, 0.45, 0.81, 0.57, 0.06)) rgb = SLOT
  const d = Math.hypot(u - 0.7, v - 0.71)
  const glow = Math.max(0, 1 - d / 0.24) ** 2 * 0.5
  if (glow > 0) rgb = rgb.map((c, i) => c + (LED[i] - c) * glow)
  if (d <= 0.1) rgb = LED
  return [rgb[0], rgb[1], rgb[2], a]
}

function render(size, { full = false, scale = 1 } = {}) {
  const ss = 4
  const px = Buffer.alloc(size * size * 4)
  for (let py = 0; py < size; py++) {
    for (let pxl = 0; pxl < size; pxl++) {
      let r = 0
      let g = 0
      let b = 0
      let a = 0
      for (let sy = 0; sy < ss; sy++) {
        for (let sx = 0; sx < ss; sx++) {
          const c = shade((pxl + (sx + 0.5) / ss) / size, (py + (sy + 0.5) / ss) / size, full, scale)
          r += c[0] * c[3]
          g += c[1] * c[3]
          b += c[2] * c[3]
          a += c[3]
        }
      }
      const o = (py * size + pxl) * 4
      if (a > 0) {
        px[o] = Math.round(r / a)
        px[o + 1] = Math.round(g / a)
        px[o + 2] = Math.round(b / a)
      }
      px[o + 3] = Math.round((a / (ss * ss)) * 255)
    }
  }
  return px
}

const CRC = (() => {
  const t = new Uint32Array(256)
  for (let n = 0; n < 256; n++) {
    let c = n
    for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1
    t[n] = c >>> 0
  }
  return t
})()
const crc32 = (buf) => {
  let c = 0xffffffff
  for (const byte of buf) c = CRC[(c ^ byte) & 0xff] ^ (c >>> 8)
  return (c ^ 0xffffffff) >>> 0
}
const chunk = (type, data) => {
  const len = Buffer.alloc(4)
  len.writeUInt32BE(data.length)
  const body = Buffer.concat([Buffer.from(type), data])
  const crc = Buffer.alloc(4)
  crc.writeUInt32BE(crc32(body))
  return Buffer.concat([len, body, crc])
}
function png(size, pixels) {
  const ihdr = Buffer.alloc(13)
  ihdr.writeUInt32BE(size, 0)
  ihdr.writeUInt32BE(size, 4)
  ihdr[8] = 8 // bit depth
  ihdr[9] = 6 // RGBA
  const rows = Buffer.alloc(size * (size * 4 + 1))
  for (let y = 0; y < size; y++) {
    pixels.copy(rows, y * (size * 4 + 1) + 1, y * size * 4, (y + 1) * size * 4)
  }
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(rows, { level: 9 })), chunk('IEND', Buffer.alloc(0))])
}

const files = [
  ['icon-192.png', 192, {}],
  ['icon-512.png', 512, {}],
  // Maskable: full-bleed background with the mark inside the central safe zone.
  ['icon-maskable-512.png', 512, { full: true, scale: 0.72 }],
  // iOS fills transparent corners with black and rounds the square itself.
  ['apple-touch-icon.png', 180, { full: true, scale: 0.86 }],
]
for (const [name, size, opts] of files) {
  const data = png(size, render(size, opts))
  writeFileSync(join(out, name), data)
  console.log(`${name}  ${size}x${size}  ${data.length} bytes`)
}
