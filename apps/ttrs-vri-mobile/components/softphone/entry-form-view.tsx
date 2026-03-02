import { Label } from "@react-navigation/elements";
import { Image } from "expo-image";
import React, { useEffect, useRef } from "react";
import { View } from "react-native";
import Animated, { FadeInRight, FadeOutRight } from "react-native-reanimated";

import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Text } from "@/components/ui/text";
import { EntryMode } from "@/store/entry-store";
import { styles } from "@/styles/components/entry-form-view.styles";

/** เปิดใช้ static ข้อมูลฟอร์มสำหรับ dev (prefill ชื่อ/เบอร์/หน่วยงาน) */
const DEV_PREFILL_FORM = __DEV__;

const DEV_FORM_VALUES: { fullName: string; phone: string; department: string } = {
  fullName: "ผู้ใช้ทดสอบ",
  phone: "0812345678",
  department: "หน่วยงานทดสอบ",
};

export interface EntryFormViewProps {
  mode: Exclude<EntryMode, null>;
  values: { fullName: string; phone: string; department: string };
  errors: { fullName?: string; phone?: string; department?: string };
  isLoading: boolean;
  errorMessage: string | null;
  onChange: (field: keyof { fullName: string; phone: string; department: string }, value: string) => void;
  onSubmit: () => void;
}

export function EntryFormView({ mode, values, errors, isLoading, errorMessage, onChange, onSubmit }: EntryFormViewProps) {
  const hasPrefilled = useRef(false);

  useEffect(() => {
    if (!DEV_PREFILL_FORM || hasPrefilled.current) return;
    hasPrefilled.current = true;
    onChange("fullName", DEV_FORM_VALUES.fullName);
    onChange("phone", DEV_FORM_VALUES.phone);
    onChange("department", DEV_FORM_VALUES.department);
  }, [onChange]);

  return (
    <Animated.View entering={FadeInRight.duration(320)} exiting={FadeOutRight.duration(240)} style={styles.container}>
      <View style={styles.inputStack}>
        <View>
          {mode === "normal" ? (
            <Image source={require("@/assets/images/drawable-xxhdpi-v4/ic_speeddial_pic1.png")} contentFit="contain" style={styles.inputFormIcon} />
          ) : (
            <Image source={require("@/assets/images/drawable-xxhdpi-v4/ic_speeddial_pic2.png")} contentFit="contain" style={styles.inputFormIcon} />
          )}

          <Label style={styles.inputFormIconContainer}>
            {mode === "normal" ? "สนทนาวิดีโอ" : "สนทนาวิดีโอฉุกเฉิน"}
          </Label>
          <Label style={styles.inputFormIconContainer}>(ติดต่อขอใช้บริการล่ามภาษามือทางไกล)</Label>
        </View>
        <Animated.View entering={FadeInRight.duration(300).delay(0)}>
          <Input
            label="ชื่อ-นามสกุล"
            value={values.fullName}
            editable={!isLoading}
            onChangeText={(text) => onChange("fullName", text)}
            error={errors.fullName}
            placeholder=""
            labelStyle={styles.label}
            style={styles.inputField}
          />
        </Animated.View>
        <Animated.View entering={FadeInRight.duration(300).delay(80)}>
          <Input
            label="เลขหมายโทรศัพท์เพื่อติดต่อกลับ"
            value={values.phone}
            editable={!isLoading}
            keyboardType="phone-pad"
            onChangeText={(text) => onChange("phone", text)}
            error={errors.phone}
            placeholder=""
            labelStyle={styles.label}
            style={styles.inputField}
          />
        </Animated.View>
        <Animated.View entering={FadeInRight.duration(300).delay(160)}>
          <Input
            label="หน่วยงาน"
            value={values.department}
            editable={!isLoading}
            onChangeText={(text) => onChange("department", text)}
            error={errors.department}
            placeholder=""
            labelStyle={styles.label}
            style={styles.inputField}
          />
        </Animated.View>
      </View>

      {errorMessage ? <Text style={styles.errorText}>{errorMessage}</Text> : null}

      <Animated.View entering={FadeInRight.duration(300).delay(240)}>
        <Button
          variant="success"
          size="lg"
          style={[styles.submitButton, mode === "emergency" ? styles.submitButtonEmergency : null]}
          disabled={isLoading}
          onPress={onSubmit}
        >
          {isLoading ? "กำลังเชื่อมต่อ..." : "เข้าใช้งาน"}
        </Button>
      </Animated.View>
    </Animated.View>
  );
}
