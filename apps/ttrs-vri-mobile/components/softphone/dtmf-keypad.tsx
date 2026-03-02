import React from "react";
import { Pressable, Vibration, View } from "react-native";

import { ms, s } from "@/lib/scale";
import { styles } from "@/styles/components/dtmf-keypad.styles";

import { AppIcon } from "../ui/icon";
import { Text } from "../ui/text";

interface DtmfKeypadProps {
  visible: boolean;
  onClose: () => void;
  onDigitPress: (digit: string) => void;
}

export function DtmfKeypad({ visible, onClose, onDigitPress }: DtmfKeypadProps) {
  if (!visible) return null;

  const handlePress = (digit: string) => {
    Vibration.vibrate(10);
    onDigitPress(digit);
  };

  const rows = [
    ["1", "2", "3"],
    ["4", "5", "6"],
    ["7", "8", "9"],
    ["*", "0", "#"],
  ];

  return (
    <View style={styles.overlay}>
      <Pressable style={styles.backdrop} onPress={onClose} />
      <View style={styles.container}>
        <View style={styles.header}>
          <Text style={styles.title}>Keypad</Text>
          <Pressable onPress={onClose} style={styles.closeButton} hitSlop={s(16)}>
            <AppIcon name="x" size={ms(24)} color="#fff" />
          </Pressable>
        </View>

        <View style={styles.grid}>
          {rows.map((row, rowIndex) => (
            <View key={rowIndex} style={styles.row}>
              {row.map((digit) => (
                <Pressable key={digit} style={({ pressed }) => [styles.button, pressed && styles.buttonPressed]} onPress={() => handlePress(digit)}>
                  <Text style={styles.digit}>{digit}</Text>
                </Pressable>
              ))}
            </View>
          ))}
        </View>
      </View>
    </View>
  );
}
