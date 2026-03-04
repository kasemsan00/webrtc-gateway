export class MediaStream {
  getTracks() {
    return [];
  }
  getAudioTracks() {
    return [];
  }
  getVideoTracks() {
    return [];
  }
  addTrack() {}
}

export class RTCIceCandidate {}

export class RTCPeerConnection {}

export class RTCSessionDescription {
  constructor(public value: unknown) {
    void value;
  }
}

export const mediaDevices = {
  getUserMedia: async () => ({
    getTracks: () => [],
    getAudioTracks: () => [],
    getVideoTracks: () => [],
  }),
};

