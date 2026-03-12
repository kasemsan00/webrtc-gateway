export type DateTimeFormatOptions = Intl.DateTimeFormatOptions

const DEFAULT_TIME_ZONE = 'Asia/Bangkok'

export function formatThaiDateTime(
  iso: string,
  options: DateTimeFormatOptions = {},
): string {
  if (!iso) return '-'
  try {
    return new Date(iso).toLocaleString('th-TH', {
      hour12: false,
      timeZone: DEFAULT_TIME_ZONE,
      ...options,
    })
  } catch {
    return iso
  }
}
