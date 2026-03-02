import React, { useCallback } from "react";
import { Pressable, TextInput, View } from "react-native";

import { Text } from "@/components/ui/text";
import { styles } from "@/styles/components/live-text-composer.styles";

interface LiveTextComposerProps {
  value: string;
  onChangeText: (text: string) => void;
  onSelectionChange: (start: number, end: number) => void;
  onSendReset?: () => void;
}

export function LiveTextComposer({ value, onChangeText, onSelectionChange, onSendReset }: LiveTextComposerProps) {
  const handleSelection = useCallback(
    (event: { nativeEvent: { selection: { start: number; end: number } } }) => {
      const { start, end } = event.nativeEvent.selection;
      onSelectionChange(start, end);
    },
    [onSelectionChange],
  );

  return (
    <View style={styles.container}>
      <Text style={styles.title}>Live Text</Text>
      <TextInput
        style={styles.input}
        multiline
        value={value}
        onChangeText={onChangeText}
        onSelectionChange={handleSelection}
        placeholder="Type to send RTT..."
        placeholderTextColor="#94A3B8"
        textAlignVertical="top"
      />
      {onSendReset && (
        <Pressable style={styles.resetButton} onPress={onSendReset}>
          <Text style={styles.resetText}>Clear</Text>
        </Pressable>
      )}
    </View>
  );
}
