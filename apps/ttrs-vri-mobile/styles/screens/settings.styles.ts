import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "#0F172A",
  },
  scrollView: {
    flex: 1,
  },
  scrollContent: {
    flexGrow: 1,
  },
  contentWrapper: {
    width: "100%",
  },
  contentWrapperTablet: {
    maxWidth: 720,
    alignSelf: "center",
  },
  header: {
    paddingHorizontal: 24,
    paddingTop: 16,
    paddingBottom: 8,
  },
  headerTitle: {
    fontSize: 32,
    fontWeight: "700",
    color: "#F1F5F9",
  },
  statusOverview: {
    flexDirection: "row",
    marginHorizontal: 20,
    marginVertical: 16,
    padding: 16,
    backgroundColor: "rgba(30, 41, 59, 0.6)",
    borderRadius: 16,
  },
  statusItem: {
    flex: 1,
    alignItems: "center",
    gap: 4,
  },
  statusIndicator: {
    width: 12,
    height: 12,
    borderRadius: 6,
    marginBottom: 4,
  },
  statusLabel: {
    fontSize: 12,
    color: "#64748B",
    fontWeight: "500",
  },
  statusValue: {
    fontSize: 14,
    fontWeight: "600",
  },
  statusDivider: {
    width: 1,
    backgroundColor: "rgba(51, 65, 85, 0.5)",
    marginHorizontal: 16,
  },
  section: {
    paddingHorizontal: 20,
    marginBottom: 24,
  },
  sectionTitle: {
    fontSize: 14,
    fontWeight: "600",
    color: "#64748B",
    textTransform: "uppercase",
    letterSpacing: 1,
    marginBottom: 12,
    paddingHorizontal: 4,
  },
  card: {
    backgroundColor: "rgba(30, 41, 59, 0.6)",
    borderColor: "rgba(51, 65, 85, 0.3)",
    borderRadius: 16,
  },
  cardContent: {
    padding: 16,
  },
  inputGroup: {
    marginBottom: 16,
  },
  inputLabel: {
    fontSize: 13,
    fontWeight: "600",
    color: "#94A3B8",
    marginBottom: 8,
  },
  input: {
    height: 48,
    backgroundColor: "rgba(15, 23, 42, 0.6)",
    borderRadius: 10,
    paddingHorizontal: 16,
    fontSize: 16,
    color: "#F1F5F9",
    borderWidth: 1,
    borderColor: "rgba(51, 65, 85, 0.5)",
  },
  errorText: {
    fontSize: 13,
    color: "#EF4444",
    marginBottom: 16,
  },
  inputHint: {
    fontSize: 11,
    color: "#64748B",
    marginTop: 6,
    fontStyle: "italic",
  },
  buttonRow: {
    flexDirection: "row",
    gap: 12,
    marginTop: 8,
  },
  flex1: {
    flex: 1,
  },
  button: {
    height: 48,
    borderRadius: 10,
    justifyContent: "center",
    alignItems: "center",
  },
  buttonPrimary: {
    backgroundColor: "#10B981",
  },
  buttonDanger: {
    backgroundColor: "#EF4444",
  },
  buttonDisabled: {
    backgroundColor: "#334155",
    opacity: 0.6,
  },
  buttonText: {
    fontSize: 15,
    fontWeight: "600",
    color: "#fff",
  },
  buttonOutline: {
    borderWidth: 1,
    borderColor: "#6366F1",
    backgroundColor: "transparent",
  },
  buttonOutlineText: {
    fontSize: 15,
    fontWeight: "600",
    color: "#6366F1",
  },
  settingDivider: {
    height: 1,
    backgroundColor: "rgba(51, 65, 85, 0.3)",
  },
  resolutionPicker: {
    flexDirection: "row",
    gap: 8,
  },
  resolutionOption: {
    flex: 1,
    paddingVertical: 12,
    paddingHorizontal: 12,
    borderRadius: 10,
    backgroundColor: "rgba(15, 23, 42, 0.6)",
    borderWidth: 1,
    borderColor: "rgba(51, 65, 85, 0.5)",
    alignItems: "center",
  },
  resolutionOptionSelected: {
    backgroundColor: "rgba(99, 102, 241, 0.2)",
    borderColor: "#6366F1",
  },
  resolutionOptionText: {
    fontSize: 13,
    fontWeight: "600",
    color: "#94A3B8",
  },
  resolutionOptionTextSelected: {
    color: "#F1F5F9",
  },
  resolutionBitrateText: {
    fontSize: 10,
    fontWeight: "500",
    color: "#64748B",
    marginTop: 2,
  },
  resolutionBitrateTextSelected: {
    color: "#A5B4FC",
  },
});
