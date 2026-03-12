import type { VideoConfig } from '../types'

export function buildIceServers(turnConfig: {
  url: string
  username: string
  credential: string
}) {
  if (!turnConfig.url) return []
  return [
    {
      urls: turnConfig.url,
      username: turnConfig.username,
      credential: turnConfig.credential,
    },
  ]
}

export async function waitForIceGatheringComplete(
  pc: RTCPeerConnection,
  timeoutMs = 2000,
) {
  if (pc.iceGatheringState === 'complete') return

  await new Promise<void>((resolve) => {
    let done = false
    const timeout = setTimeout(() => {
      if (done) return
      done = true
      pc.removeEventListener('icegatheringstatechange', onChange)
      resolve()
    }, timeoutMs)

    function onChange() {
      if (pc.iceGatheringState !== 'complete' || done) return
      done = true
      clearTimeout(timeout)
      pc.removeEventListener('icegatheringstatechange', onChange)
      resolve()
    }

    pc.addEventListener('icegatheringstatechange', onChange)
  })
}

export function applyH264CodecPreference(
  pc: RTCPeerConnection,
  useConstrainedBaseline: boolean,
) {
  const capabilities = RTCRtpSender.getCapabilities('video')
  const codecs = capabilities?.codecs ?? []
  let h264Codecs = codecs.filter(
    (codec) =>
      codec.mimeType === 'video/H264' &&
      codec.sdpFmtpLine?.includes('packetization-mode=1'),
  )

  if (useConstrainedBaseline) {
    const constrained = h264Codecs.filter((codec) =>
      codec.sdpFmtpLine?.includes('profile-level-id=42e0'),
    )
    if (constrained.length > 0) {
      h264Codecs = constrained
    }
  }

  if (h264Codecs.length === 0) return

  for (const transceiver of pc.getTransceivers()) {
    if (transceiver.receiver.track.kind === 'video') {
      transceiver.setCodecPreferences(h264Codecs)
    }
  }
}

export async function applyVideoConstraints(
  pc: RTCPeerConnection,
  localStream: MediaStream,
  videoConfig: VideoConfig,
) {
  const senders = pc
    .getSenders()
    .filter((sender) => sender.track && sender.track.kind === 'video')

  for (const sender of senders) {
    const parameters = sender.getParameters()
    if (parameters.encodings.length === 0) {
      parameters.encodings = [{}]
    }

    parameters.encodings[0].maxBitrate = videoConfig.maxBitrate * 1000
    parameters.encodings[0].maxFramerate = videoConfig.maxFramerate
    await sender.setParameters(parameters)
  }

  const videoTrack = localStream.getVideoTracks()[0]
  await videoTrack.applyConstraints({
    width: { ideal: videoConfig.width },
    height: { ideal: videoConfig.height },
    frameRate: { max: videoConfig.maxFramerate },
  })
}

export function formatRtcStats(report: {
  roundTripTime?: number
  packetsLost?: number
  packetsReceived?: number
  frameWidth?: number
  frameHeight?: number
  bytesReceived?: number
}) {
  const loss =
    report.packetsLost && report.packetsReceived
      ? ((report.packetsLost / report.packetsReceived) * 100).toFixed(1)
      : '0'

  return {
    rttMs: report.roundTripTime
      ? `${Math.round(report.roundTripTime * 1000)}`
      : '-',
    packetLossPercent: loss,
    resolution:
      report.frameWidth && report.frameHeight
        ? `${report.frameWidth}x${report.frameHeight}`
        : '-',
  }
}
