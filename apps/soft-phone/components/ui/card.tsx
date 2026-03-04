import * as React from "react";
import { StyleSheet, View, type ViewProps } from "react-native";
import { Text } from "./text";

const Card = React.forwardRef<View, ViewProps>(({ style, ...props }, ref) => <View ref={ref} style={[styles.card, style]} {...props} />);
Card.displayName = "Card";

const CardHeader = React.forwardRef<View, ViewProps>(({ style, ...props }, ref) => <View ref={ref} style={[styles.header, style]} {...props} />);
CardHeader.displayName = "CardHeader";

// CardTitle - Text component doesn't support ref forwarding
function CardTitle({ style, ...props }: React.ComponentProps<typeof Text>) {
  return <Text style={[styles.title, style]} {...props} />;
}
CardTitle.displayName = "CardTitle";

// CardDescription - Text component doesn't support ref forwarding
function CardDescription({ style, ...props }: React.ComponentProps<typeof Text>) {
  return <Text style={[styles.description, style]} {...props} />;
}
CardDescription.displayName = "CardDescription";

const CardContent = React.forwardRef<View, ViewProps>(({ style, ...props }, ref) => <View ref={ref} style={[styles.content, style]} {...props} />);
CardContent.displayName = "CardContent";

const CardFooter = React.forwardRef<View, ViewProps>(({ style, ...props }, ref) => <View ref={ref} style={[styles.footer, style]} {...props} />);
CardFooter.displayName = "CardFooter";

export { Card, CardContent, CardDescription, CardFooter, CardHeader, CardTitle };

const styles = StyleSheet.create({
  card: {
    borderRadius: 16,
    borderWidth: 1,
    borderColor: "rgba(44, 44, 46, 0.3)",
    backgroundColor: "rgba(28, 28, 30, 0.6)",
  },
  header: {
    padding: 16,
    gap: 6,
  },
  content: {
    padding: 16,
    paddingTop: 0,
  },
  footer: {
    padding: 16,
    paddingTop: 0,
    flexDirection: "row",
    alignItems: "center",
  },
  title: {
    fontSize: 20,
    fontWeight: "700",
    color: "#F2F2F7",
  },
  description: {
    fontSize: 14,
    color: "#8E8E93",
  },
});
