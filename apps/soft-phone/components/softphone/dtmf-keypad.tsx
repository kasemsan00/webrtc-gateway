import { styles } from "@/styles/components/DtmfKeypad.styles";
import React, { useEffect, useRef, useState } from "react";
import { Animated, Easing, Pressable, Vibration, View } from "react-native";

import { ms, s } from "@/lib/scale";

import { AppIcon } from "../ui/icon";
import { Text } from "../ui/text";

interface DtmfKeypadProps {
  visible: boolean;
  onClose: () => void;
  onDigitPress: (digit: string) => void;
}

const ANIMATION_DURATION = 220;

export function DtmfKeypad({ visible, onClose, onDigitPress }: DtmfKeypadProps) {
  const [isMounted, setIsMounted] = useState(visible);
  const animation = useRef(new Animated.Value(visible ? 1 : 0)).current;

  useEffect(() => {
    if (visible) {
      setIsMounted(true);
      Animated.timing(animation, {
        toValue: 1,
        duration: ANIMATION_DURATION,
        easing: Easing.out(Easing.cubic),
        useNativeDriver: true,
      }).start();
    } else {
      Animated.timing(animation, {
        toValue: 0,
        duration: ANIMATION_DURATION,
        easing: Easing.in(Easing.cubic),
        useNativeDriver: true,
      }).start(({ finished }) => {
        if (finished) {
          setIsMounted(false);
        }
      });
    }
  }, [animation, visible]);

  if (!isMounted) {
    return null;
  }

  const overlayStyle = {
    opacity: animation,
  };

  const containerStyle = {
    transform: [
      {
        translateY: animation.interpolate({
          inputRange: [0, 1],
          outputRange: [100, 0],
        }),
      },
    ],
  };

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
    <Animated.View style={[styles.overlay, overlayStyle]} pointerEvents={visible ? "auto" : "none"}>
      <Pressable style={styles.backdrop} onPress={onClose} />
      <Animated.View style={[styles.container, containerStyle]}>
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
      </Animated.View>
    </Animated.View>
  );
}
