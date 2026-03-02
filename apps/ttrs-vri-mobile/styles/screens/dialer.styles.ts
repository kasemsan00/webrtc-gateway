import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  container: {
    flex: 1,
    backgroundColor: "red",
  },
  scrollView: {
    flex: 1,
    backgroundColor: "white",
  },
  header: {
    flexDirection: "row",
    justifyContent: "flex-start",
    alignItems: "center",
    paddingHorizontal: 24,
    paddingTop: 8,
    paddingBottom: 4,
    backgroundColor: "#26495c",
  },
  backButton: {
    alignSelf: "flex-start",
  },
  statusBadge: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 12,
    paddingVertical: 6,
    borderRadius: 16,
    gap: 8,
  },
  statusDot: {
    width: 8,
    height: 8,
    borderRadius: 4,
  },
  formContainer: {
    flexGrow: 1,
    paddingHorizontal: 24,
    paddingBottom: 40,
    justifyContent: "center",
    gap: 20,
  },
  branding: {
    alignItems: "center",
    gap: 8,
  },
  logo: {
    width: 88,
    height: 88,
  },
  title: {
    fontSize: 24,
    color: "#111827",
    fontWeight: "600",
    textAlign: "center",
  },
  subtitle: {
    fontSize: 13,
    color: "#4B5563",
    textAlign: "center",
  },
  helperText: {
    fontSize: 12,
    color: "#6B7280",
    textAlign: "center",
  },
  contentSection: {
    width: "100%",
  },
});
