import { StyleSheet } from "react-native";

import { ms, s } from "@/lib/scale";

export const styles = StyleSheet.create({
  overlay: {
    position: "absolute",
    top: 0,
    bottom: 0,
    left: 0,
    right: 0,
    backgroundColor: "rgba(0,0,0,0.5)",
    justifyContent: "flex-end",
    zIndex: 20,
  },
  backdrop: {
    ...StyleSheet.absoluteFillObject,
  },
  container: {
    backgroundColor: "#1E293B",
    borderTopLeftRadius: s(20),
    borderTopRightRadius: s(20),
    paddingBottom: ms(40),
    paddingTop: ms(16),
  },
  header: {
    flexDirection: "row",
    justifyContent: "space-between",
    alignItems: "center",
    paddingHorizontal: s(20),
    paddingBottom: ms(16),
    borderBottomWidth: 1,
    borderBottomColor: "rgba(255,255,255,0.1)",
    marginBottom: ms(20),
  },
  title: {
    fontSize: ms(18),
    fontWeight: "600",
    color: "#fff",
  },
  closeButton: {
    padding: s(4),
    opacity: 0.8,
  },
  grid: {
    gap: ms(20),
    paddingHorizontal: s(40),
  },
  row: {
    flexDirection: "row",
    justifyContent: "space-between",
  },
  button: {
    width: s(72),
    height: s(72),
    borderRadius: s(36),
    backgroundColor: "rgba(51, 65, 85, 0.8)",
    justifyContent: "center",
    alignItems: "center",
  },
  buttonPressed: {
    backgroundColor: "rgba(99, 102, 241, 0.5)",
  },
  digit: {
    fontSize: ms(32),
    fontWeight: "600",
    color: "#fff",
  },
});
