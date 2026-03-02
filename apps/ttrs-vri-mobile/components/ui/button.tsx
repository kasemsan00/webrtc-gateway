import * as React from "react";
import { Pressable, StyleSheet, View, type PressableProps, type TextStyle, type ViewStyle } from "react-native";

import { Text } from "./text";

type ButtonVariant = "default" | "destructive" | "outline" | "secondary" | "ghost" | "link" | "success" | "dialpad" | "call" | "endCall";
type ButtonSize = "default" | "sm" | "lg" | "xl" | "icon" | "dialpad" | "call";

type ButtonProps = PressableProps & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  textStyle?: TextStyle;
};

const Button = React.forwardRef<View, ButtonProps>(({ variant = "default", size = "default", style, textStyle, children, disabled, ...props }, ref) => {
  return (
    <Pressable
      ref={ref}
      disabled={disabled}
      style={(state) => {
        const { pressed } = state;
        const base: ViewStyle[] = [styles.base, variantStyles[variant], sizeStyles[size], disabled ? styles.disabled : null, pressed ? styles.pressed : null].filter(
          Boolean
        ) as ViewStyle[];

        const userStyle = typeof style === "function" ? style(state) : style;
        return [base, userStyle];
      }}
      {...props}
    >
      {typeof children === "string" ? <Text style={[textBaseStyles.base, textVariantStyles[variant], textSizeStyles[size], textStyle]}>{children}</Text> : children}
    </Pressable>
  );
});

Button.displayName = "Button";

const styles = StyleSheet.create({
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

const variantStyles = StyleSheet.create({
  default: { backgroundColor: "#6366F1" },
  destructive: { backgroundColor: "#EF4444" },
  outline: { backgroundColor: "transparent", borderWidth: 2, borderColor: "rgba(51, 65, 85, 0.7)" },
  secondary: { backgroundColor: "#1E293B" },
  ghost: { backgroundColor: "transparent" },
  link: { backgroundColor: "transparent" },
  success: { backgroundColor: "#10B981" },
  dialpad: { backgroundColor: "#1E293B" },
  call: { backgroundColor: "#10B981" },
  endCall: { backgroundColor: "#EF4444" },
});

const sizeStyles = StyleSheet.create({
  default: { height: 48, paddingHorizontal: 24 },
  sm: { height: 36, paddingHorizontal: 12 },
  lg: { height: 56, paddingHorizontal: 32 },
  xl: { height: 64, paddingHorizontal: 40 },
  icon: { height: 48, width: 48 },
  dialpad: { height: 80, width: 80, borderRadius: 40 },
  call: { height: 64, width: 64, borderRadius: 32 },
});

const textBaseStyles = StyleSheet.create({
  base: { fontWeight: "600" },
});

const textVariantStyles = StyleSheet.create({
  default: { color: "#ffffff" },
  destructive: { color: "#ffffff" },
  outline: { color: "#F1F5F9" },
  secondary: { color: "#F1F5F9" },
  ghost: { color: "#F1F5F9" },
  link: { color: "#6366F1", textDecorationLine: "underline" },
  success: { color: "#ffffff" },
  dialpad: { color: "#ffffff" },
  call: { color: "#ffffff" },
  endCall: { color: "#ffffff" },
});

const textSizeStyles = StyleSheet.create({
  default: { fontSize: 16 },
  sm: { fontSize: 14 },
  lg: { fontSize: 18 },
  xl: { fontSize: 20 },
  icon: { fontSize: 16 },
  dialpad: { fontSize: 24 },
  call: { fontSize: 20 },
});

export { Button };
export type { ButtonProps, ButtonSize, ButtonVariant };
