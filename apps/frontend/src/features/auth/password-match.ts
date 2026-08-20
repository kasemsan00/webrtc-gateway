export function passwordsMatch(submitted: string, expected: string): boolean {
  if (submitted.length === 0 || expected.length === 0) {
    return false
  }
  if (submitted.length !== expected.length) {
    return false
  }

  let diff = 0
  for (let i = 0; i < submitted.length; i += 1) {
    diff |= submitted.charCodeAt(i) ^ expected.charCodeAt(i)
  }
  return diff === 0
}
