import * as React from "react";
import { StyleSheet, TextInput, type TextInputProps, View } from "react-native";
import { Text } from "./text";

export interface InputProps extends TextInputProps {
  label?: string;
  error?: string;
}

const Input = React.forwardRef<TextInput, InputProps>(({ label, error, placeholderTextColor, style, ...props }, ref) => {
  return (
    <View style={styles.container}>
      {label && <Text style={styles.label}>{label}</Text>}
      <TextInput
        ref={ref}
        style={[styles.input, error ? styles.inputError : null, style]}
        placeholderTextColor={placeholderTextColor ?? "#9CA3AF"}
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
    color: "#F2F2F7",
  },
  input: {
    height: 48,
    borderRadius: 12,
    borderWidth: 1,
    borderColor: "rgba(44, 44, 46, 0.5)",
    backgroundColor: "rgba(17, 17, 17, 0.6)",
    paddingHorizontal: 16,
    paddingVertical: 10,
    fontSize: 16,
    color: "#F2F2F7",
  },
  inputError: {
    borderColor: "#EF4444",
  },
  error: {
    marginTop: 6,
    fontSize: 13,
    color: "#EF4444",
  },
});
