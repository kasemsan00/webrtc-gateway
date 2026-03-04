import React from "react";
import { View } from "react-native";

import { Text } from "@/components/ui/text";
import { styles } from "@/styles/components/LiveTextViewer.styles";

interface LiveTextViewerProps {
  text: string;
  isTyping: boolean;
}

export function LiveTextViewer({ text, isTyping }: LiveTextViewerProps) {
  return (
    <View style={styles.container}>
      <Text style={styles.title}>Incoming RTT</Text>
      <View style={styles.textContainer}>
        <Text style={styles.text}>{text || " "}</Text>
      </View>
      {isTyping && <Text style={styles.typing}>Typing...</Text>}
    </View>
  );
}
