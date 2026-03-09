import { URL, fileURLToPath } from 'node:url'
import { defineConfig } from 'vite'
import { devtools } from '@tanstack/devtools-vite'
import { tanstackStart } from '@tanstack/react-start/plugin/vite'
import viteReact from '@vitejs/plugin-react'
import viteTsConfigPaths from 'vite-tsconfig-paths'

import tailwindcss from '@tailwindcss/vite'
import netlify from '@netlify/vite-plugin-tanstack-start'

const isNetlifyBuild = process.env.NETLIFY === 'true'
const isVitest = process.env.VITEST === 'true'

const config = defineConfig({
  resolve: {
    alias: {
      '@': fileURLToPath(new URL('./src', import.meta.url)),
    },
  },
  plugins: [
    ...(isVitest ? [] : [devtools()]),
    ...(isVitest || !isNetlifyBuild ? [] : [netlify()]),
    // this is the plugin that enables path aliases
    viteTsConfigPaths({
      projects: ['./tsconfig.json'],
    }),
    tailwindcss(),
    ...(isVitest ? [] : [tanstackStart()]),
    viteReact(),
  ],
  test: {
    environment: 'jsdom',
    include: [
      'src/**/*.test.ts',
      'src/**/*.test.tsx',
      'src/**/*.smoke.test.ts',
      'src/**/*.smoke.test.tsx',
    ],
    coverage: {
      provider: 'v8',
      reporter: ['text', 'json-summary', 'html'],
      reportsDirectory: './coverage',
      include: ['src/**/*.ts', 'src/**/*.tsx'],
      exclude: [
        'src/**/*.test.ts',
        'src/**/*.test.tsx',
        'src/**/*.smoke.test.ts',
        'src/**/*.smoke.test.tsx',
        'src/routeTree.gen.ts',
      ],
      thresholds: {
        lines: 10,
        functions: 10,
        statements: 10,
        branches: 5,
      },
    },
  },
})

export default config
