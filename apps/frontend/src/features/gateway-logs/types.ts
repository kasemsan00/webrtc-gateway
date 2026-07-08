export interface LogFile {
  name: string
  size: number
  modifiedAt: string
  current: boolean
}

export interface LogFileListResponse {
  items: Array<LogFile>
}

export interface LogTailResponse {
  name: string
  current: boolean
  tail: number
  lines: Array<string>
  truncated: boolean
}
