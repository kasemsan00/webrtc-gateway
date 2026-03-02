import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  container: {
    width: "100%",
    gap: 170,
  },
  buttonWrapper: {
    width: "100%",
    position: "relative",
  },
  modeButtonTextNormal: {
    fontSize: 20,
    fontWeight: "500",
    color: "#000",
    position: "absolute",
    textAlign: "center",
    bottom: 18,
    right: 70,
  },
  modeButtonTextEmergency: {
    fontSize: 20,
    fontWeight: "500",
    color: "#000",
    position: "absolute",
    textAlign: "center",
    bottom: 14,
    left: 120,
  },
  modeImageNormal: {
    width: 300,
    height: 200,
    marginBottom: 12,
  },
  modeImageEmergency: {
    width: 350,
    height: 200,
    marginBottom: 12,
  },
  modeButtonNormalTextContainer: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
  },
  modeButtonEmergencyTextContainer: {
    position: "absolute",
    top: 0,
    left: 0,
    right: 0,
    bottom: 0,
  },
  modeButtonContainer: {
    width: 430,
    position: "relative",
  },
});
