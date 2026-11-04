import type { SessionEvent, SessionStats } from './types'

export type EventDirection = 'out' | 'in' | null
export type EventSeverity = 'error' | 'warning' | 'success' | 'info'

/**
 * Direction of a SIP event relative to the gateway, from the event name:
 * `sip_invite_sent` → out (gateway → far end), `sip_bye_received` → in.
 */
export function eventDirection(name: string): EventDirection {
  if (/_(sent|send_request)$/.test(name)) return 'out'
  if (/_received$/.test(name)) return 'in'
  return null
}

const ERROR_NAME = /(failed|error|timeout|rejected|auth_failed)/
const WARNING_NAME = /(cancel|warning|renegotiation_started)/

export function eventSeverity(
  event: Pick<SessionEvent, 'name' | 'sipStatusCode'>,
): EventSeverity {
  const code = event.sipStatusCode ?? 0
  // 401/407 are normal digest-auth challenges, not failures.
  if (code === 401 || code === 407) return 'info'
  if (code >= 400) return 'error'
  if (ERROR_NAME.test(event.name)) return 'error'
  if (WARNING_NAME.test(event.name)) return 'warning'
  if (code >= 200 && code < 300) return 'success'
  return 'info'
}

/** `sip_invite_sent` → `INVITE sent`; other names get spaces. */
export function humanizeEventName(name: string): string {
  const match = /^sip_([a-z]+)_(.+)$/.exec(name)
  const methods = new Set([
    'invite',
    'ack',
    'bye',
    'cancel',
    'options',
    'message',
    'reinvite',
    'info',
    'register',
  ])
  if (match && methods.has(match[1])) {
    return `${match[1].toUpperCase()} ${match[2].replace(/_/g, ' ')}`
  }
  return name.replace(/^sip_/, 'SIP ').replace(/_/g, ' ')
}

/** Seconds since `baseIso` with millisecond precision, e.g. "+1.234s". */
export function formatOffset(iso: string, baseIso: string | undefined): string {
  if (!baseIso) return ''
  const diff = Date.parse(iso) - Date.parse(baseIso)
  if (Number.isNaN(diff)) return ''
  const sign = diff < 0 ? '-' : '+'
  return `${sign}${(Math.abs(diff) / 1000).toFixed(3)}s`
}

export interface Insight {
  tone: 'success' | 'warning' | 'danger' | 'muted'
  title: string
  detail?: string
}

/**
 * Plain-language reading of the most recent stats sample so non-media experts
 * can tell whether audio/video looked healthy.
 */
export function interpretStats(
  samples: Array<SessionStats>,
  now: number = Date.now(),
): Array<Insight> {
  if (samples.length === 0) {
    return [
      {
        tone: 'muted',
        title: 'No media stats recorded for this call',
        detail:
          'Stats are written while media flows. Very short or failed calls have none.',
      },
    ]
  }
  const latest = [...samples].sort(
    (a, b) => Date.parse(b.timestamp) - Date.parse(a.timestamp),
  )[0]
  const insights: Array<Insight> = []

  if (latest.audioRtcpRr === 0 && latest.audioRtcpSr === 0) {
    insights.push({
      tone: 'warning',
      title: 'No audio RTCP reports recorded',
      detail:
        'The far end never sent RTCP for audio. Possible one-way audio, NAT/firewall blocking RTCP, or an endpoint without RTCP.',
    })
  } else {
    insights.push({
      tone: 'success',
      title: 'Audio RTCP flowing',
      detail: `${latest.audioRtcpRr} receiver / ${latest.audioRtcpSr} sender reports.`,
    })
  }

  const hasVideo =
    latest.videoRtcpRr > 0 ||
    latest.videoRtcpSr > 0 ||
    latest.pliSent > 0 ||
    Boolean(latest.lastKeyframeAt)
  if (hasVideo) {
    if (latest.pliSent > 0 && latest.pliResponse === 0) {
      insights.push({
        tone: 'danger',
        title: `Keyframe requests unanswered (${latest.pliSent} PLI sent, 0 answered)`,
        detail:
          'The gateway asked for a video keyframe but none arrived. Video is likely frozen or black on the WebRTC side.',
      })
    } else if (latest.pliSent > 0) {
      const ratio = latest.pliResponse / latest.pliSent
      insights.push({
        tone: ratio < 0.5 ? 'warning' : 'success',
        title: `${latest.pliResponse}/${latest.pliSent} keyframe requests answered`,
        detail:
          ratio < 0.5
            ? 'Fewer than half of PLI requests produced a keyframe; expect slow video recovery after loss.'
            : 'The sender responds to keyframe requests.',
      })
    }
    if (latest.lastKeyframeAt) {
      const ageSec = (now - Date.parse(latest.lastKeyframeAt)) / 1000
      const sampleAgeSec = (now - Date.parse(latest.timestamp)) / 1000
      // Only meaningful while the call is (nearly) live.
      if (sampleAgeSec < 60 && ageSec > 30) {
        insights.push({
          tone: 'warning',
          title: `Last keyframe ${Math.round(ageSec)}s ago`,
          detail: 'No recent keyframe: a viewer joining now may see no video.',
        })
      }
    }
    if (latest.videoRtcpRr === 0 && latest.videoRtcpSr === 0) {
      insights.push({
        tone: 'warning',
        title: 'No video RTCP reports recorded',
      })
    }
  }
  return insights
}
