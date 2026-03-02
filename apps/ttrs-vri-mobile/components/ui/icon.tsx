import {
  BellOff,
  Bot,
  ChevronRight,
  Cog,
  Delete,
  Grid3x3,
  MapPin,
  Mic,
  MicOff,
  Minus,
  Pause,
  Phone,
  PhoneCall,
  PhoneOff,
  Plus,
  RefreshCw,
  Video,
  VideoOff,
  Volume2,
  VolumeX,
  X,
  type LucideIcon,
} from "lucide-react-native";
import React from "react";
import { StyleProp, ViewStyle } from "react-native";

/**
 * Type-safe icon names that map to Lucide icons
 */
export type AppIconName =
  | "chevronRight"
  | "phone"
  | "phoneOff"
  | "phoneCall"
  | "mic"
  | "micOff"
  | "dialpad"
  | "volume2"
  | "volumeX"
  | "plus"
  | "pause"
  | "video"
  | "videoOff"
  | "bot"
  | "bellOff"
  | "refreshCw"
  | "settings"
  | "delete"
  | "x"
  | "mapPin"
  | "minus";

/**
 * Mapping of AppIcon names to Lucide icon components
 */
const ICON_MAP: Record<AppIconName, LucideIcon> = {
  chevronRight: ChevronRight,
  phone: Phone,
  phoneOff: PhoneOff,
  phoneCall: PhoneCall,
  mic: Mic,
  micOff: MicOff,
  dialpad: Grid3x3,
  volume2: Volume2,
  volumeX: VolumeX,
  plus: Plus,
  pause: Pause,
  video: Video,
  videoOff: VideoOff,
  bot: Bot,
  bellOff: BellOff,
  refreshCw: RefreshCw,
  settings: Cog,
  delete: Delete,
  x: X,
  mapPin: MapPin,
  minus: Minus,
};

export interface AppIconProps {
  name: AppIconName;
  size?: number;
  color?: string;
  style?: StyleProp<ViewStyle>;
}

/**
 * AppIcon component that wraps Lucide icons with a consistent API
 */
export function AppIcon({ name, size = 24, color = "#000", style }: AppIconProps) {
  const IconComponent = ICON_MAP[name];

  if (!IconComponent) {
    console.warn(`Icon "${name}" not found in Lucide icons`);
    return null;
  }

  return <IconComponent size={size} color={color} style={style} />;
}

/**
 * Helper to get a Lucide icon component directly (useful for tabBarIcon)
 */
export function getLucideIcon(name: AppIconName) {
  return ICON_MAP[name];
}
