import React from "react";
import { View } from "react-native";

import { Text } from "@/components/ui/text";
import { styles } from "@/styles/components/live-text-viewer.styles";

interface LiveTextViewerProps {
  text: string;
  isTyping: boolean;
}

export function LiveTextViewer({ text, isTyping }: LiveTextViewerProps) {
  return (
    <View style={styles.container}>
      <View style={styles.textContainer}>
        <Text style={styles.text}>{text || " "}</Text>
      </View>
      {/* {isTyping && <Text style={styles.typing}>Typing…</Text>} */}
    </View>
  );
}
