import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  base: {
    flexDirection: "row",
    alignItems: "center",
    justifyContent: "center",
    borderRadius: 12,
  },
  pressed: {
    opacity: 0.85,
  },
  disabled: {
    opacity: 0.5,
  },
});

export const variantStyles = StyleSheet.create({
  default: { backgroundColor: "#48484A" },
  destructive: { backgroundColor: "#EF4444" },
  outline: { backgroundColor: "transparent", borderWidth: 2, borderColor: "rgba(44, 44, 46, 0.7)" },
  secondary: { backgroundColor: "#1C1C1E" },
  ghost: { backgroundColor: "transparent" },
  link: { backgroundColor: "transparent" },
  success: { backgroundColor: "#10B981" },
  dialpad: { backgroundColor: "#1C1C1E" },
  call: { backgroundColor: "#10B981" },
  endCall: { backgroundColor: "#EF4444" },
});

export const sizeStyles = StyleSheet.create({
  default: { height: 48, paddingHorizontal: 24 },
  sm: { height: 36, paddingHorizontal: 12 },
  lg: { height: 56, paddingHorizontal: 32 },
  xl: { height: 64, paddingHorizontal: 40 },
  icon: { height: 48, width: 48 },
  dialpad: { height: 80, width: 80, borderRadius: 40 },
  call: { height: 64, width: 64, borderRadius: 32 },
});

export const textBaseStyles = StyleSheet.create({
  base: { fontWeight: "600" },
});

export const textVariantStyles = StyleSheet.create({
  default: { color: "#ffffff" },
  destructive: { color: "#ffffff" },
  outline: { color: "#F2F2F7" },
  secondary: { color: "#F2F2F7" },
  ghost: { color: "#F2F2F7" },
  link: { color: "#8E8E93", textDecorationLine: "underline" },
  success: { color: "#ffffff" },
  dialpad: { color: "#ffffff" },
  call: { color: "#ffffff" },
  endCall: { color: "#ffffff" },
});

export const textSizeStyles = StyleSheet.create({
  default: { fontSize: 16 },
  sm: { fontSize: 14 },
  lg: { fontSize: 18 },
  xl: { fontSize: 20 },
  icon: { fontSize: 16 },
  dialpad: { fontSize: 24 },
  call: { fontSize: 20 },
});
