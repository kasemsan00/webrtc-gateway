import { createServerFn } from '@tanstack/react-start'

import { passwordsMatch } from './password-match'

function readSubmittedPassword(data: unknown): { password: string } {
  if (typeof data !== 'object' || data === null || !('password' in data)) {
    return { password: '' }
  }
  const password = data.password
  return { password: typeof password === 'string' ? password : '' }
}

export const verifyFrontendPassword = createServerFn({
  method: 'POST',
})
  .inputValidator(readSubmittedPassword)
  .handler((ctx) => {
    const expected = process.env.FRONTEND_PASSWORD?.trim() ?? ''
    return {
      ok: passwordsMatch(ctx.data.password, expected),
    }
  })
