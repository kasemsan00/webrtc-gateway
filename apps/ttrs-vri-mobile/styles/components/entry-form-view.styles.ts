import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  container: {
    width: "100%",
  },
  inputFormIcon: {
    width: 100,
    height: 100,
    alignSelf: "center",
    marginBottom: 16,
  },
  inputFormIconContainer: {
    flexDirection: "row",
    alignItems: "center",
    gap: 16,
    color: "#2687c8",
    fontSize: 16,
    fontWeight: "600",
  },
  inputStack: {
    gap: 16,
  },
  label: {
    fontSize: 16,
    fontWeight: "600",
  },
  inputField: {
    fontSize: 18,
    borderRadius: 0,
  },
  errorText: {
    fontSize: 16,
    color: "#B91C1C",
    textAlign: "center",
    marginBottom: 8,
  },
  submitButton: {
    width: "100%",
    borderRadius: 0,
    backgroundColor: "#0384e2",
    marginTop: 50,
  },
  submitButtonEmergency: {
    backgroundColor: "#DC2626",
  },
});
