import { createServer } from 'node:http'
import { createReadStream } from 'node:fs'
import { stat } from 'node:fs/promises'
import { extname, join, resolve } from 'node:path'
import { Readable } from 'node:stream'

import app from './dist/server/server.js'

const host = process.env.HOST ?? '0.0.0.0'
const port = Number(process.env.PORT ?? 4173)
const staticRoot = resolve(process.cwd(), 'dist/client')

const mimeTypes = {
  '.css': 'text/css; charset=utf-8',
  '.ico': 'image/x-icon',
  '.js': 'text/javascript; charset=utf-8',
  '.json': 'application/json; charset=utf-8',
  '.png': 'image/png',
  '.svg': 'image/svg+xml',
  '.txt': 'text/plain; charset=utf-8',
}

const getMimeType = (filePath) =>
  mimeTypes[extname(filePath).toLowerCase()] ?? 'application/octet-stream'

const toStaticPath = (pathname) => {
  const relativePath = pathname.replace(/^\/+/, '')
  const absolutePath = resolve(staticRoot, relativePath)
  if (!absolutePath.startsWith(staticRoot)) return null
  return absolutePath
}

const serveStaticFile = async (req, res, url) => {
  if (req.method !== 'GET' && req.method !== 'HEAD') return false
  if (url.pathname === '/' || url.pathname.endsWith('/')) return false

  const filePath = toStaticPath(url.pathname)
  if (!filePath) return false

  try {
    const file = await stat(filePath)
    if (!file.isFile()) return false

    res.statusCode = 200
    res.setHeader('content-type', getMimeType(filePath))
    if (url.pathname.startsWith('/assets/')) {
      res.setHeader('cache-control', 'public, max-age=31536000, immutable')
    }

    if (req.method === 'HEAD') {
      res.end()
      return true
    }

    createReadStream(filePath).pipe(res)
    return true
  } catch {
    return false
  }
}

createServer(async (req, res) => {
  try {
    const origin = `http://${req.headers.host ?? `localhost:${port}`}`
    const url = new URL(req.url ?? '/', origin)

    const staticServed = await serveStaticFile(req, res, url)
    if (staticServed) return

    const hasBody = req.method !== 'GET' && req.method !== 'HEAD'
    const request = new Request(url, {
      method: req.method,
      headers: req.headers,
      body: hasBody ? req : undefined,
      duplex: hasBody ? 'half' : undefined,
    })

    const response = await app.fetch(request)
    res.statusCode = response.status

    response.headers.forEach((value, key) => {
      res.setHeader(key, value)
    })

    if (!response.body) {
      res.end()
      return
    }

    Readable.fromWeb(response.body).pipe(res)
  } catch (error) {
    res.statusCode = 500
    res.setHeader('content-type', 'text/plain; charset=utf-8')
    res.end('Internal Server Error')
    console.error(error)
  }
}).listen(port, host, () => {
  console.log(`Server listening on http://${host}:${port}`)
})
