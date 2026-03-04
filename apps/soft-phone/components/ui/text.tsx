import * as React from "react";
import { Platform, Text as RNText, StyleSheet, type Role, type TextProps } from "react-native";

type TextVariant = "default" | "h1" | "h2" | "h3" | "h4" | "p" | "blockquote" | "code" | "lead" | "large" | "small" | "muted";

const ROLE: Partial<Record<TextVariant, Role>> = {
  h1: "heading",
  h2: "heading",
  h3: "heading",
  h4: "heading",
  blockquote: Platform.select({ web: "blockquote" as Role }),
  code: Platform.select({ web: "code" as Role }),
};

const ARIA_LEVEL: Partial<Record<TextVariant, string>> = {
  h1: "1",
  h2: "2",
  h3: "3",
  h4: "4",
};

type Props = TextProps & {
  variant?: TextVariant;
};

function Text({ variant = "default", style, ...props }: Props) {
  return <RNText role={ROLE[variant]} aria-level={ARIA_LEVEL[variant]} style={[styles.base, variantStyles[variant], style]} {...props} />;
}

const styles = StyleSheet.create({
  base: {
    fontSize: 16,
    color: "#F2F2F7",
    ...Platform.select({
      web: {
        userSelect: "text",
      },
    }),
  },
});

const variantStyles = StyleSheet.create({
  default: {},
  h1: { fontSize: 36, fontWeight: "800", textAlign: "center" },
  h2: { fontSize: 30, fontWeight: "700" },
  h3: { fontSize: 24, fontWeight: "700" },
  h4: { fontSize: 20, fontWeight: "700" },
  p: { marginTop: 12, lineHeight: 22 },
  blockquote: { marginTop: 16, paddingLeft: 12, borderLeftWidth: 2, borderLeftColor: "rgba(142, 142, 147, 0.35)", fontStyle: "italic" },
  code: {
    fontFamily: Platform.select({ ios: "Menlo", android: "monospace", default: "monospace" }),
    fontSize: 13,
    fontWeight: "600",
    backgroundColor: "rgba(99, 99, 102, 0.15)",
    paddingHorizontal: 6,
    paddingVertical: 3,
    borderRadius: 6,
  },
  lead: { fontSize: 20, color: "#8E8E93" },
  large: { fontSize: 18, fontWeight: "600" },
  small: { fontSize: 14, fontWeight: "600" },
  muted: { fontSize: 14, color: "#8E8E93" },
});

export { Text };
export type { TextVariant };
