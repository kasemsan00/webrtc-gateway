import { createServer } from 'node:http'
import { createReadStream } from 'node:fs'
import { stat } from 'node:fs/promises'
import { extname, resolve } from 'node:path'
import { Readable } from 'node:stream'

import app from './dist/server/server.js'

const host = process.env.HOST ?? '0.0.0.0'
const port = Number(process.env.PORT ?? 4173)
const frontendPassword = (process.env.FRONTEND_PASSWORD ?? '').trim()
if (!frontendPassword) {
  console.error('FRONTEND_PASSWORD is required')
  process.exit(1)
}
const staticRoot = resolve(process.cwd(), 'dist/client')
const runtimeEnvKeys = [
  'VITE_GATEWAY_URL',
  'VITE_CONFIG_AUTORECORD',
  'VITE_BASE_PATH',
]

const normalizeBasePath = (raw) => {
  const trimmed = (raw ?? '').trim()
  if (!trimmed || trimmed === '/') return '/'
  const withLeadingSlash = trimmed.startsWith('/') ? trimmed : `/${trimmed}`
  return withLeadingSlash.replace(/\/+$/, '') || '/'
}

const basePath = normalizeBasePath(
  process.env.VITE_BASE_PATH ?? process.env.BASE_PATH,
)
const basePathPrefix = basePath === '/' ? '' : basePath

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

const stripBasePath = (pathname) => {
  if (!basePathPrefix) return pathname
  if (pathname === basePathPrefix) return '/'
  if (pathname.startsWith(`${basePathPrefix}/`)) {
    return pathname.slice(basePathPrefix.length) || '/'
  }
  return null
}

const toStaticPath = (pathname) => {
  const relativePath = pathname.replace(/^\/+/, '')
  const absolutePath = resolve(staticRoot, relativePath)
  if (!absolutePath.startsWith(staticRoot)) return null
  return absolutePath
}

const escapeInlineScriptJson = (value) =>
  value
    .replace(/</g, '\\u003c')
    .replace(/>/g, '\\u003e')
    .replace(/&/g, '\\u0026')
    .replace(/\u2028/g, '\\u2028')
    .replace(/\u2029/g, '\\u2029')

const buildRuntimeEnvScript = () => {
  const runtimeEnv = {}

  for (const key of runtimeEnvKeys) {
    const value = process.env[key]
    if (value !== undefined && value !== '') {
      runtimeEnv[key] = value
    }
  }

  if (!runtimeEnv.VITE_BASE_PATH && basePathPrefix) {
    runtimeEnv.VITE_BASE_PATH = `${basePathPrefix}/`
  }

  const serialized = escapeInlineScriptJson(JSON.stringify(runtimeEnv))
  return `<script>window.__APP_RUNTIME_ENV__=${serialized};</script>`
}

const injectRuntimeEnvIntoHtml = (html) => {
  const script = buildRuntimeEnvScript()
  if (html.includes('</head>')) {
    return html.replace('</head>', `${script}</head>`)
  }
  return `${script}${html}`
}

const serveStaticFile = async (req, res, pathname) => {
  if (req.method !== 'GET' && req.method !== 'HEAD') return false
  if (pathname === '/' || pathname.endsWith('/')) return false

  const filePath = toStaticPath(pathname)
  if (!filePath) return false

  try {
    const file = await stat(filePath)
    if (!file.isFile()) return false

    res.statusCode = 200
    res.setHeader('content-type', getMimeType(filePath))
    if (pathname.startsWith('/assets/')) {
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

const writeFetchResponse = async (req, res, url) => {
  const hasBody = req.method !== 'GET' && req.method !== 'HEAD'
  const request = new Request(url, {
    method: req.method,
    headers: req.headers,
    body: hasBody ? req : undefined,
    duplex: hasBody ? 'half' : undefined,
  })

  const response = await app.fetch(request)
  const contentType = response.headers.get('content-type') ?? ''
  const isHtmlResponse = contentType.includes('text/html')

  if (isHtmlResponse) {
    const html = await response.text()
    const injectedHtml = injectRuntimeEnvIntoHtml(html)

    res.statusCode = response.status
    response.headers.forEach((value, key) => {
      if (key.toLowerCase() === 'content-length') return
      res.setHeader(key, value)
    })
    res.end(injectedHtml)
    return
  }

  res.statusCode = response.status
  response.headers.forEach((value, key) => {
    res.setHeader(key, value)
  })

  if (!response.body) {
    res.end()
    return
  }

  Readable.fromWeb(response.body).pipe(res)
}

createServer(async (req, res) => {
  try {
    const origin = `http://${req.headers.host ?? `localhost:${port}`}`
    const url = new URL(req.url ?? '/', origin)

    if (basePathPrefix) {
      if (url.pathname === basePathPrefix) {
        res.statusCode = 302
        res.setHeader('location', `${basePathPrefix}/`)
        res.end()
        return
      }

      const stripped = stripBasePath(url.pathname)
      if (stripped === null) {
        res.statusCode = 404
        res.setHeader('content-type', 'text/plain; charset=utf-8')
        res.end('Not Found')
        return
      }

      // Static assets are stored without the public base prefix on disk.
      const staticServed = await serveStaticFile(req, res, stripped)
      if (staticServed) return

      // Keep the public URL (with base path) for the SSR router.
      await writeFetchResponse(req, res, url)
      return
    }

    const staticServed = await serveStaticFile(req, res, url.pathname)
    if (staticServed) return

    await writeFetchResponse(req, res, url)
  } catch (error) {
    res.statusCode = 500
    res.setHeader('content-type', 'text/plain; charset=utf-8')
    res.end('Internal Server Error')
    console.error(error)
  }
}).listen(port, host, () => {
  const publicBase = basePathPrefix || '/'
  console.log(`Server listening on http://${host}:${port}${publicBase}`)
})
