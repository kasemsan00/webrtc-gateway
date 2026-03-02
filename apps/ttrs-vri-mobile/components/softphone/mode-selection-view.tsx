import React from "react";
import Animated, { FadeInLeft, FadeOutLeft, useAnimatedStyle, useSharedValue, withSpring } from "react-native-reanimated";

import { Button } from "@/components/ui/button";
import { styles } from "@/styles/components/mode-selection-view.styles";
import { Image } from "expo-image";
import { Text } from "../ui/text";

export interface ModeSelectionViewProps {
  onSelectMode: (mode: "normal" | "emergency") => void;
  disabled?: boolean;
}

export function ModeSelectionView({ onSelectMode, disabled }: ModeSelectionViewProps) {
  const normalScale = useSharedValue(1);
  const emergencyScale = useSharedValue(1);

  const normalAnimatedStyle = useAnimatedStyle(() => ({
    transform: [{ scale: normalScale.value }],
    display: "flex",
    justifyContent: "center",
    alignItems: "center",
  }));

  const emergencyAnimatedStyle = useAnimatedStyle(() => ({
    transform: [{ scale: emergencyScale.value }],
    display: "flex",
    justifyContent: "center",
    alignItems: "center",
  }));

  const handleNormalPressIn = () => {
    normalScale.value = withSpring(0.95, { damping: 15, stiffness: 400 });
  };

  const handleNormalPressOut = () => {
    normalScale.value = withSpring(1, { damping: 15, stiffness: 400 });
  };

  const handleEmergencyPressIn = () => {
    emergencyScale.value = withSpring(0.95, { damping: 15, stiffness: 400 });
  };

  const handleEmergencyPressOut = () => {
    emergencyScale.value = withSpring(1, { damping: 15, stiffness: 400 });
  };

  return (
    <Animated.View entering={FadeInLeft.duration(320)} exiting={FadeOutLeft.duration(240)} style={styles.container}>
      <Animated.View style={[normalAnimatedStyle]}>
        <Button
          style={styles.modeButtonNormalTextContainer}
          variant="ghost"
          onPressIn={handleNormalPressIn}
          onPressOut={handleNormalPressOut}
          onPress={() => {
            handleNormalPressOut();
            onSelectMode("normal");
          }}
          disabled={disabled}
        >
          <Image
            source={require("@/assets/images/drawable-xxhdpi-v4/vdo_conversation_background.png")}
            contentFit="contain"
            style={styles.modeImageNormal}
          />
          <Text style={styles.modeButtonTextNormal}>สนทนาวิดีโอ</Text>
        </Button>
      </Animated.View>

      <Animated.View style={[emergencyAnimatedStyle]}>
        <Button
          style={styles.modeButtonEmergencyTextContainer}
          variant="ghost"
          onPressIn={handleEmergencyPressIn}
          onPressOut={handleEmergencyPressOut}
          onPress={() => {
            handleEmergencyPressOut();
            onSelectMode("emergency");
          }}
          disabled={disabled}
        >
          <Image
            source={require("@/assets/images/drawable-xxhdpi-v4/emer_vdo_conversation_background.png")}
            contentFit="contain"
            style={styles.modeImageEmergency}
          />
          <Text style={styles.modeButtonTextEmergency}>ฉุกเฉิน</Text>
        </Button>
      </Animated.View>
    </Animated.View>
  );
}
