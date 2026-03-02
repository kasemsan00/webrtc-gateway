import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  locationBar: {
    backgroundColor: "#26495c",
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 12,
    paddingBottom: 6,
  },
  locationIcon: {
    width: 20,
    height: 20,
    marginRight: 8,
  },
  locationText: {
    flex: 1,
    fontSize: 12,
    color: "white",
  },
  headerOverlay: {
    position: "absolute",
    left: 0,
    right: 0,
    zIndex: 20,
    elevation: 20,
  },
  header: {
    flexDirection: "row",
    alignItems: "center",
    paddingHorizontal: 4,
    paddingVertical: 4,
    backgroundColor: "#26495c",
  },
  backButton: {
    alignSelf: "flex-start",
  },
  footerLogo: {
    backgroundColor: "white",
    display: "flex",
    flexDirection: "row",
    justifyContent: "center",
    alignItems: "center",
    paddingVertical: 14,
    paddingBottom: 32,
  },
});
