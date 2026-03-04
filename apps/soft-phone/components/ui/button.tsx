import { sizeStyles, styles, textBaseStyles, textSizeStyles, textVariantStyles, variantStyles } from "@/styles/components/Button.styles";
import * as React from "react";
import { Pressable, View, type PressableProps, type TextStyle, type ViewStyle } from "react-native";

import { Text } from "./text";

type ButtonVariant = "default" | "destructive" | "outline" | "secondary" | "ghost" | "link" | "success" | "dialpad" | "call" | "endCall";
type ButtonSize = "default" | "sm" | "lg" | "xl" | "icon" | "dialpad" | "call";

type ButtonProps = PressableProps & {
  variant?: ButtonVariant;
  size?: ButtonSize;
  textStyle?: TextStyle;
};

const Button = React.forwardRef<View, ButtonProps>(
  ({ variant = "default", size = "default", style, textStyle, children, disabled, ...props }, ref) => {
    return (
      <Pressable
        ref={ref}
        disabled={disabled}
        style={(state) => {
          const { pressed } = state;
          const base: ViewStyle[] = [
            styles.base,
            variantStyles[variant],
            sizeStyles[size],
            disabled ? styles.disabled : null,
            pressed ? styles.pressed : null,
          ].filter(Boolean) as ViewStyle[];

          const userStyle = typeof style === "function" ? style(state) : style;
          return [base, userStyle];
        }}
        {...props}
      >
        {typeof children === "string" ? (
          <Text style={[textBaseStyles.base, textVariantStyles[variant], textSizeStyles[size], textStyle]}>{children}</Text>
        ) : (
          children
        )}
      </Pressable>
    );
  },
);

Button.displayName = "Button";

export { Button };
export type { ButtonProps, ButtonSize, ButtonVariant };
