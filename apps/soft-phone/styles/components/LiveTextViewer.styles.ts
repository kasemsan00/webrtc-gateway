import { ms, mvs, s } from "@/lib/scale";
import { StyleSheet } from "react-native";

export const styles = StyleSheet.create({
  container: {
    backgroundColor: "rgba(0,0,0,0.66)",
    borderRadius: s(12),
    paddingHorizontal: s(12),
    paddingVertical: mvs(10),
    gap: mvs(6),
  },
  title: {
    fontSize: ms(12),
    color: "rgba(255,255,255,0.65)",
  },
  textContainer: {
    minHeight: mvs(34),
  },
  text: {
    fontSize: ms(16),
    color: "#fff",
  },
  typing: {
    fontSize: ms(12),
    color: "#A5F3FC",
  },
});
