import * as React from "react";
import { StyleSheet, TextInput, View, type TextInputProps, type TextStyle } from "react-native";
import { Text } from "./text";

export interface InputProps extends TextInputProps {
  label?: string;
  error?: string;
  labelStyle?: TextStyle;
}

const Input = React.forwardRef<TextInput, InputProps>(({ label, error, labelStyle, placeholderTextColor, style, ...props }, ref) => {
  return (
    <View style={styles.container}>
      {label && <Text style={[styles.label, labelStyle]}>{label}</Text>}
      <TextInput
        ref={ref}
        style={[styles.input, error ? styles.inputError : null, style]}
        placeholderTextColor={placeholderTextColor ?? "#6B7280"}
        {...props}
      />
      {error && <Text style={styles.error}>{error}</Text>}
    </View>
  );
});

Input.displayName = "Input";

export { Input };

const styles = StyleSheet.create({
  container: {
    width: "100%",
  },
  label: {
    marginBottom: 8,
    fontSize: 14,
    fontWeight: "600",
    color: "#111827",
  },
  input: {
    height: 48,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: "#D1D5DB",
    backgroundColor: "#FFFFFF",
    paddingHorizontal: 16,
    paddingVertical: 10,
    fontSize: 16,
    color: "#111827",
  },
  inputError: {
    borderColor: "#DC2626",
  },
  error: {
    marginTop: 6,
    fontSize: 13,
    color: "#B91C1C",
  },
});
