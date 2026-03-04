import { useIsFocused } from "@react-navigation/native";
import React, { useEffect } from "react";
import { StyleSheet, type ViewProps } from "react-native";
import Animated, { Easing, useAnimatedStyle, useSharedValue, withTiming } from "react-native-reanimated";

interface AnimatedTabScreenProps extends ViewProps {
  children: React.ReactNode;
}

const ANIMATION_DURATION = 200;
const TRANSLATE_Y_OFFSET = 8;

export function AnimatedTabScreen({ children, style, ...props }: AnimatedTabScreenProps) {
  const isFocused = useIsFocused();
  const opacity = useSharedValue(0);
  const translateY = useSharedValue(TRANSLATE_Y_OFFSET);

  useEffect(() => {
    if (isFocused) {
      opacity.value = withTiming(1, {
        duration: ANIMATION_DURATION,
        easing: Easing.out(Easing.ease),
      });
      translateY.value = withTiming(0, {
        duration: ANIMATION_DURATION,
        easing: Easing.out(Easing.ease),
      });
    } else {
      // Reset immediately so next focus animates in
      opacity.value = 0;
      translateY.value = TRANSLATE_Y_OFFSET;
    }
  }, [isFocused, opacity, translateY]);

  const animatedStyle = useAnimatedStyle(() => ({
    opacity: opacity.value,
    transform: [{ translateY: translateY.value }],
  }));

  return (
    <Animated.View style={[styles.container, animatedStyle, style]} {...props}>
      {children}
    </Animated.View>
  );
}

const styles = StyleSheet.create({
  container: {
    flex: 1,
  },
});
