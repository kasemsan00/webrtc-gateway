import { useThemeColors } from "@/hooks/use-theme-color";
import { createDialpadStyles } from "@/styles/components/Dialpad.styles";
import React, { useMemo, useState } from "react";
import { LayoutChangeEvent, Pressable, View, useWindowDimensions } from "react-native";

import { Text } from "../ui/text";

interface DialpadProps {
  onPress: (value: string) => void;
  onLongPress?: (value: string) => void;
  onMetricsChange?: (metrics: { keySize: number; gap: number }) => void;
  style?: any;
}

const KEYS = [
  [
    { main: "1", sub: "" },
    { main: "2", sub: "" },
    { main: "3", sub: "" },
  ],
  [
    { main: "4", sub: "" },
    { main: "5", sub: "" },
    { main: "6", sub: "" },
  ],
  [
    { main: "7", sub: "" },
    { main: "8", sub: "" },
    { main: "9", sub: "" },
  ],
  [
    { main: "*", sub: "" },
    { main: "0", sub: "" },
    { main: "#", sub: "" },
  ],
];

const MIN_KEY = 56;
const MAX_KEY_PHONE = 96;
const MAX_KEY_TABLET = 90; // Increased from 20 to sensible tablet size
const MIN_GAP = 12;
const MAX_GAP = 22;

export function Dialpad({ onPress, onLongPress, onMetricsChange, style }: DialpadProps) {
  const colors = useThemeColors();
  const styles = useMemo(() => createDialpadStyles(colors), [colors]);
  const { width: windowWidth, height: windowHeight } = useWindowDimensions();
  const [layout, setLayout] = useState({ width: 0, height: 0 });

  const { keySize, gap, mainFontSize, subFontSize, letterSpacing } = useMemo(() => {
    // Use container layout if available, otherwise fallback to window
    // (fallback is mainly for initial render before onLayout fires)
    const availableWidth = layout.width || windowWidth;
    const availableHeight = layout.height || windowHeight * 0.5;

    const minDimension = Math.min(windowWidth, windowHeight);
    const isTablet = minDimension >= 600;

    const maxKeySize = isTablet ? MAX_KEY_TABLET : MAX_KEY_PHONE;

    // 1. Calculate Gap first (responsive to width)
    const rawGap = Math.round(availableWidth * 0.06);
    const clampedGap = Math.max(MIN_GAP, Math.min(MAX_GAP, rawGap));

    // 2. Calculate Key Size based on WIDTH (3 columns)
    // Formula: (Width - gap*2) / 3
    // Note: We assume padding is handled by parent or internal gap logic
    const widthBasedKeySize = Math.floor((availableWidth - clampedGap * 2) / 3);

    // 3. Calculate Key Size based on HEIGHT (4 rows)
    // Formula: (Height - gap*3) / 4
    const heightBasedKeySize = Math.floor((availableHeight - clampedGap * 3) / 4);

    // 4. Use the smaller of the two to ensure it fits in both dimensions
    const rawKeySize = Math.min(widthBasedKeySize, heightBasedKeySize);
    const clampedKeySize = Math.max(MIN_KEY, Math.min(maxKeySize, rawKeySize));

    // Notify parent about metrics (for call/backspace buttons)
    if (onMetricsChange) {
      // Defer to avoid render-cycle warning
      setTimeout(() => onMetricsChange({ keySize: clampedKeySize, gap: clampedGap }), 0);
    }

    const rawMainFontSize = Math.round(clampedKeySize * 0.45);
    const clampedMainFontSize = Math.max(24, Math.min(48, rawMainFontSize));

    const rawSubFontSize = Math.round(clampedKeySize * 0.14);
    const clampedSubFontSize = Math.max(10, Math.min(16, rawSubFontSize));

    const rawLetterSpacing = Math.max(1, Math.min(3, clampedSubFontSize * 0.12));

    return {
      keySize: clampedKeySize,
      gap: clampedGap,
      mainFontSize: clampedMainFontSize,
      subFontSize: clampedSubFontSize,
      letterSpacing: rawLetterSpacing,
    };
  }, [layout.width, layout.height, windowWidth, windowHeight, onMetricsChange]);

  const onLayout = (e: LayoutChangeEvent) => {
    const { width, height } = e.nativeEvent.layout;
    // Only update if dimensions changed significantly (>1px) to avoid loops
    if (Math.abs(width - layout.width) > 1 || Math.abs(height - layout.height) > 1) {
      setLayout({ width, height });
    }
  };

  return (
    <View style={[styles.container, { gap }, style]} onLayout={onLayout}>
      {KEYS.map((row, rowIndex) => (
        <View key={rowIndex} style={[styles.row, { gap }]}>
          {row.map((key) => (
            <Pressable
              key={key.main}
              style={({ pressed }) => [
                styles.key,
                {
                  width: keySize,
                  height: keySize,
                  borderRadius: keySize / 2,
                },
                pressed && styles.keyPressed,
              ]}
              onPress={() => onPress(key.main)}
              onLongPress={() => {
                if (key.main === "0") {
                  onPress("+");
                } else if (onLongPress) {
                  onLongPress(key.main);
                }
              }}
            >
              <Text style={[styles.keyMain, { fontSize: mainFontSize }]}>{key.main}</Text>
              {key.sub ? (
                <Text
                  style={[
                    styles.keySub,
                    {
                      fontSize: subFontSize,
                      letterSpacing,
                      marginTop: Math.round(keySize * 0.02),
                    },
                  ]}
                >
                  {key.sub}
                </Text>
              ) : null}
            </Pressable>
          ))}
        </View>
      ))}
    </View>
  );
}
