import path from 'node:path';

import { defineConfig } from 'vitest/config';

export default defineConfig({
  test: {
    environment: 'node',
    globals: true,
    include: ['apps/soft-phone/__tests__/**/*.test.ts'],
    clearMocks: true,
    restoreMocks: true,
  },
  resolve: {
    alias: {
      '@': path.resolve(__dirname),
      'react-native': path.resolve(
        __dirname,
        '__tests__/mocks/react-native.ts',
      ),
      'react-native-webrtc': path.resolve(
        __dirname,
        '__tests__/mocks/react-native-webrtc.ts',
      ),
    },
  },
});
