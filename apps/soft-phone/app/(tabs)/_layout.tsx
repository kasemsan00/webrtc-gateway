import { Tabs } from "expo-router";
import { ClockIcon, GripIcon, SettingsIcon } from "lucide-react-native";
import React, { useMemo } from "react";
import { Platform, Pressable, StyleSheet, View, useWindowDimensions } from "react-native";
import { useSafeAreaInsets } from "react-native-safe-area-context";

import { Text } from "@/components/ui/text";
import { useThemeColors } from "@/hooks/use-theme-color";
import type { ThemeColors } from "@/theme";

const SIDEBAR_WIDTH = 60;

export default function TabLayout() {
  const colors = useThemeColors();
  const dynamicStyles = useMemo(() => createTabStyles(colors), [colors]);
  const insets = useSafeAreaInsets();
  const { width, height } = useWindowDimensions();
  const isTablet = Math.min(width, height) >= 600;

  // Calculate dynamic tab bar style based on safe area insets (Mobile only)
  const tabBarStyle = useMemo(() => {
    if (isTablet) return undefined; // Handled by custom sidebar

    const baseHeight = 80; // Base height for tab bar content (icon + label + padding)
    const basePaddingTop = 12;
    const basePaddingBottom = 12;
    // Android: some edge-to-edge/navigation modes can report bottom inset as 0.
    // Ensure we always leave a minimum buffer so the system navigation buttons don't cover the tab bar.
    const bottomInset = Platform.OS === "android" ? Math.max(insets.bottom, 12) : insets.bottom;

    return {
      ...dynamicStyles.tabBar,
      height: baseHeight + bottomInset,
      paddingTop: basePaddingTop,
      paddingBottom: basePaddingBottom + bottomInset,
      justifyContent: "flex-start" as const,
    };
  }, [insets.bottom, isTablet, dynamicStyles.tabBar]);

  const tabBarLabelStyle = useMemo(
    () => ({
      ...staticStyles.tabLabel,
      // In landscape (mobile), labels tend to sit a bit low. Nudge them up slightly.
      marginBottom: !isTablet && width > height ? 4 : 0,
    }),
    [width, height, isTablet],
  );

  return (
    <Tabs
      screenOptions={{
        headerShown: false,
        tabBarStyle: isTablet ? { display: "none" } : tabBarStyle, // Hide default tabbar on tablet
        tabBarShowLabel: true,
        tabBarActiveTintColor: colors.tabIconSelected,
        tabBarInactiveTintColor: colors.tabIconDefault,
        tabBarLabelStyle: tabBarLabelStyle,
        tabBarIconStyle: staticStyles.tabIcon,
        tabBarHideOnKeyboard: false,
        // For tablet, push content to the right to make room for sidebar
        sceneStyle: isTablet ? { marginLeft: SIDEBAR_WIDTH, backgroundColor: colors.background } : { backgroundColor: colors.background },
      }}
      tabBar={
        isTablet
          ? ({ state, descriptors, navigation }) => {
              return (
                <View
                  style={[
                    dynamicStyles.sidebar,
                    {
                      width: SIDEBAR_WIDTH,
                      paddingTop: insets.top + 24,
                      paddingBottom: insets.bottom + 24,
                    },
                  ]}
                >
                  {state.routes.map((route, index) => {
                    const { options } = descriptors[route.key];
                    const label = options.tabBarLabel !== undefined ? options.tabBarLabel : options.title !== undefined ? options.title : route.name;

                    const isFocused = state.index === index;

                    const onPress = () => {
                      const event = navigation.emit({
                        type: "tabPress",
                        target: route.key,
                        canPreventDefault: true,
                      });

                      if (!isFocused && !event.defaultPrevented) {
                        navigation.navigate(route.name, route.params);
                      }
                    };

                    const Icon =
                      route.name === "index" ? GripIcon : route.name === "history" ? ClockIcon : route.name === "settings" ? SettingsIcon : GripIcon;

                    return (
                      <Pressable
                        key={route.key}
                        onPress={onPress}
                        style={({ pressed }) => [
                          dynamicStyles.sidebarItem,
                          isFocused && dynamicStyles.sidebarItemFocused,
                          pressed && dynamicStyles.sidebarItemPressed,
                        ]}
                      >
                        {isFocused && <View style={dynamicStyles.activeIndicator} />}
                        <Icon size={24} color={isFocused ? colors.tabIconSelected : colors.tabIconDefault} />
                        <Text style={[dynamicStyles.sidebarLabel, isFocused && dynamicStyles.sidebarLabelFocused]}>{label as string}</Text>
                      </Pressable>
                    );
                  })}
                </View>
              );
            }
          : undefined
      }
    >
      <Tabs.Screen
        name="index"
        options={{
          tabBarLabel: "Keypad",
          tabBarIcon: ({ color, size }) => (
            <View pointerEvents="none">
              <GripIcon size={size} color={color} />
            </View>
          ),
        }}
      />
      <Tabs.Screen
        name="history"
        options={{
          tabBarLabel: "Recents",
          tabBarIcon: ({ color, size }) => (
            <View pointerEvents="none">
              <ClockIcon size={size} color={color} />
            </View>
          ),
        }}
      />
      <Tabs.Screen
        name="settings"
        options={{
          tabBarLabel: "Settings",
          tabBarIcon: ({ color, size }) => (
            <View pointerEvents="none">
              <SettingsIcon size={size} color={color} />
            </View>
          ),
        }}
      />
    </Tabs>
  );
}

const createTabStyles = (c: ThemeColors) =>
  StyleSheet.create({
    tabBar: {
      backgroundColor: c.tabBar,
      borderTopWidth: 0.4,
      shadowColor: "#000",
      shadowOffset: { width: 0, height: -4 },
      elevation: 16,
      borderColor: c.border,
    },
    // Tablet Sidebar Styles
    sidebar: {
      position: "absolute",
      left: 0,
      top: 0,
      bottom: 0,
      backgroundColor: c.sidebarBg,
      borderRightWidth: 1,
      borderRightColor: c.sidebarBorder,
      alignItems: "center",
      gap: 16,
      zIndex: 50,
    },
    sidebarItem: {
      width: "100%",
      height: 72,
      justifyContent: "center",
      alignItems: "center",
      gap: 8,
      position: "relative",
    },
    sidebarItemFocused: {
      backgroundColor: c.sidebarItemFocusedBg,
    },
    sidebarItemPressed: {
      backgroundColor: c.sidebarItemPressedBg,
    },
    sidebarLabel: {
      fontSize: 12,
      fontWeight: "500",
      color: c.tabIconDefault,
    },
    sidebarLabelFocused: {
      color: c.tabIconSelected,
      fontWeight: "600",
    },
    activeIndicator: {
      position: "absolute",
      left: 0,
      top: 12,
      bottom: 12,
      width: 3,
      backgroundColor: "#10B981",
      borderTopRightRadius: 3,
      borderBottomRightRadius: 3,
    },
  });

const staticStyles = StyleSheet.create({
  tabLabel: {
    fontSize: 14,
    fontWeight: "600",
  },
  tabIcon: {
    marginBottom: 6,
  },
});
