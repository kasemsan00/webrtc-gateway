import * as React from "react";
import { Image, StyleSheet, View, type ImageProps, type ViewProps } from "react-native";
import { Text } from "./text";

interface AvatarProps extends ViewProps {
  size?: "sm" | "md" | "lg" | "xl";
}

const Avatar = React.forwardRef<View, AvatarProps>(({ size = "md", style, ...props }, ref) => (
  <View ref={ref} style={[styles.base, sizeStyles[size], style]} {...props} />
));
Avatar.displayName = "Avatar";

interface AvatarImageProps extends Omit<ImageProps, "source"> {
  source?: ImageProps["source"];
}

const AvatarImage = React.forwardRef<Image, AvatarImageProps>(({ source, style, ...props }, ref) => {
  if (!source) return null;
  return <Image ref={ref} source={source} style={[styles.image, style]} {...props} />;
});
AvatarImage.displayName = "AvatarImage";

interface AvatarFallbackProps extends ViewProps {
  children: React.ReactNode;
}

const AvatarFallback = React.forwardRef<View, AvatarFallbackProps>(({ style, children, ...props }, ref) => (
  <View ref={ref} style={[styles.fallback, style]} {...props}>
    {typeof children === "string" ? <Text style={styles.fallbackText}>{children}</Text> : children}
  </View>
));
AvatarFallback.displayName = "AvatarFallback";

export { Avatar, AvatarFallback, AvatarImage };

const sizeStyles = StyleSheet.create({
  sm: { width: 32, height: 32, borderRadius: 16 },
  md: { width: 48, height: 48, borderRadius: 24 },
  lg: { width: 64, height: 64, borderRadius: 32 },
  xl: { width: 96, height: 96, borderRadius: 48 },
});

const styles = StyleSheet.create({
  base: {
    overflow: "hidden",
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "rgba(99, 99, 102, 0.35)",
  },
  image: {
    width: "100%",
    height: "100%",
    resizeMode: "cover",
  },
  fallback: {
    width: "100%",
    height: "100%",
    alignItems: "center",
    justifyContent: "center",
    backgroundColor: "#48484A",
  },
  fallbackText: {
    color: "#ffffff",
    fontWeight: "700",
  },
});
