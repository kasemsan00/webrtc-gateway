/**
 * Applies an H.264 codec preference on a video transceiver when supported by
 * the browser, keeping other codecs as fallback.
 */
export function preferH264Codecs(
  transceiver: RTCRtpTransceiver,
  direction: 'send' | 'recv' = 'send',
): void {
  if (typeof transceiver.setCodecPreferences !== 'function') return
  const caps =
    direction === 'recv' &&
    typeof RTCRtpReceiver !== 'undefined' &&
    typeof RTCRtpReceiver.getCapabilities === 'function'
      ? RTCRtpReceiver.getCapabilities('video')
      : typeof RTCRtpSender !== 'undefined' &&
          typeof RTCRtpSender.getCapabilities === 'function'
        ? RTCRtpSender.getCapabilities('video')
        : null
  if (!caps?.codecs.length) return

  const h264: Array<RTCRtpCodec> = []
  const others: Array<RTCRtpCodec> = []
  for (const codec of caps.codecs) {
    if (codec.mimeType.toLowerCase() === 'video/h264') {
      h264.push(codec)
    } else {
      others.push(codec)
    }
  }
  if (h264.length === 0) return
  try {
    transceiver.setCodecPreferences([...h264, ...others])
  } catch {
    // Some browsers disallow setCodecPreferences on certain transceiver states.
  }
}

/**
 * Rolls back an uncommitted local SDP offer without disturbing active media.
 */
export async function safeRollbackLocalDescription(
  pc: RTCPeerConnection | null,
): Promise<boolean> {
  if (!pc || pc.signalingState !== 'have-local-offer') return false
  try {
    await pc.setLocalDescription({ type: 'rollback' })
    return true
  } catch {
    return false
  }
}

/**
 * Detaches the sender track and stops a transceiver only when it was never
 * committed (`mid === null`), so rolled-back offers do not leave orphan
 * `m=video` sections while committed screen transceivers remain reusable.
 */
export async function cleanupUncommittedTransceiver(
  transceiver: RTCRtpTransceiver | null,
): Promise<void> {
  if (!transceiver) return
  try {
    await transceiver.sender.replaceTrack(null)
  } catch {
    // Ignore sender errors on closed/stopped transceivers.
  }
  if (transceiver.mid === null) {
    try {
      transceiver.stop()
    } catch {
      // Ignore stop errors if browser already rolled back transceiver.
    }
  }
}

/** Detaches a media track from a sender while preserving the transceiver. */
export async function detachTransceiverTrack(
  transceiver: RTCRtpTransceiver | null,
): Promise<void> {
  if (!transceiver) return
  try {
    await transceiver.sender.replaceTrack(null)
  } catch {
    // Ignore if peer connection is already closed.
  }
}

export function stopMediaStream(stream: MediaStream | null): void {
  if (!stream) return
  for (const track of stream.getTracks()) {
    try {
      track.stop()
    } catch {
      // Ignore already-stopped track errors.
    }
  }
}

export function formatTimeStamp(date = new Date()): string {
  const hh = String(date.getHours()).padStart(2, '0')
  const mm = String(date.getMinutes()).padStart(2, '0')
  const ss = String(date.getSeconds()).padStart(2, '0')
  const ms = String(date.getMilliseconds()).padStart(3, '0')
  return `${hh}:${mm}:${ss}.${ms}`
}
