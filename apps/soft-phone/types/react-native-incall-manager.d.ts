declare module "react-native-incall-manager" {
  export interface InCallManagerStatic {
    start(options?: { media?: string; auto?: boolean; ringback?: string }): void;
    stop(options?: { busytone?: string }): void;
    setKeepScreenOn(enable: boolean): void;
    setSpeakerphoneOn(enable: boolean): void;
    setForceSpeakerphoneOn(flag: boolean): void;
    setMicrophoneMute(mute: boolean): void;
    turnScreenOn(): void;
    turnScreenOff(): void;
    getAudioUriJS(audioType: string, fileType: string): string;
  }

  const InCallManager: InCallManagerStatic;
  export default InCallManager;
}
