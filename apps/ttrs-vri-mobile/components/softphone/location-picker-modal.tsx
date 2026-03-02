import * as Location from "expo-location";
import Constants from "expo-constants";
import { MapPin, Search } from "lucide-react-native";
import React, { useCallback, useEffect, useRef, useState } from "react";
import { ActivityIndicator, BackHandler, Modal, Platform, Pressable, TextInput, View } from "react-native";
import MapView, { PROVIDER_DEFAULT, Region } from "react-native-maps";
import { SafeAreaView } from "react-native-safe-area-context";

import { Button } from "@/components/ui/button";
import { Text } from "@/components/ui/text";
import { LocationCoordinates, SavedLocation, useLocationStore } from "@/store/location-store";
import { styles } from "@/styles/components/location-picker-modal.styles";

interface LocationPickerModalProps {
  visible: boolean;
  onClose: () => void;
  topOffset?: number;
  presentation?: "native-modal" | "inline-overlay";
}

export function LocationPickerModal({ visible, onClose, topOffset = 0, presentation = "native-modal" }: LocationPickerModalProps) {
  const setLocation = useLocationStore((s) => s.setLocation);
  const currentLocation = useLocationStore((s) => s.currentLocation);
  const isAndroid = Platform.OS === "android";

  const mapRef = useRef<MapView>(null);
  const reverseGeocodeRequestIdRef = useRef(0);
  const mapReadyRef = useRef(!isAndroid);
  const androidGoogleMapsApiKey =
    ((Constants.expoConfig?.android?.config as { googleMaps?: { apiKey?: string } } | undefined)?.googleMaps?.apiKey ?? "").trim();
  const hasAndroidMapApiKey = !isAndroid || androidGoogleMapsApiKey.length > 0;
  const initialRegion: Region = {
    latitude: currentLocation?.coordinates.latitude ?? 13.7563,
    longitude: currentLocation?.coordinates.longitude ?? 100.5018,
    latitudeDelta: 0.01,
    longitudeDelta: 0.01,
  };
  const [searchQuery, setSearchQuery] = useState("");
  const [isSearching, setIsSearching] = useState(false);
  const [hasLocationPermission, setHasLocationPermission] = useState(false);
  const [mapInitTimedOut, setMapInitTimedOut] = useState(false);
  const [selectedCoords, setSelectedCoords] = useState<LocationCoordinates>({
    latitude: initialRegion.latitude,
    longitude: initialRegion.longitude,
  });
  const [selectedAddress, setSelectedAddress] = useState<string>(currentLocation?.address ?? "");
  const [region, setRegion] = useState<Region>(initialRegion);

  useEffect(() => {
    if (visible && currentLocation) {
      setSelectedCoords(currentLocation.coordinates);
      setSelectedAddress(currentLocation.address);
      setRegion({
        latitude: currentLocation.coordinates.latitude,
        longitude: currentLocation.coordinates.longitude,
        latitudeDelta: 0.01,
        longitudeDelta: 0.01,
      });
    }
  }, [visible, currentLocation]);

  useEffect(() => {
    if (!visible) return;

    let isMounted = true;

    const refreshLocationPermission = async () => {
      try {
        const currentStatus = await Location.getForegroundPermissionsAsync();
        const status = currentStatus.status === "granted" ? currentStatus : await Location.requestForegroundPermissionsAsync();
        if (!isMounted) return;
        setHasLocationPermission(status.status === "granted");
        if (status.status !== "granted") {
          console.warn("[LocationPicker] Location permission not granted. Using coordinate fallback.");
        }
      } catch (error) {
        console.error("[LocationPicker] Failed to check location permission:", error);
        if (isMounted) {
          setHasLocationPermission(false);
        }
      }
    };

    void refreshLocationPermission();

    return () => {
      isMounted = false;
    };
  }, [visible]);

  useEffect(() => {
    if (!(presentation === "inline-overlay" && visible && isAndroid)) {
      return;
    }

    const backHandlerSubscription = BackHandler.addEventListener("hardwareBackPress", () => {
      onClose();
      return true;
    });

    return () => {
      backHandlerSubscription.remove();
    };
  }, [presentation, visible, isAndroid, onClose]);

  useEffect(() => {
    if (!visible) return;

    mapReadyRef.current = !isAndroid;
    setMapInitTimedOut(false);

    if (!isAndroid || !hasAndroidMapApiKey) {
      return;
    }

    const timeout = setTimeout(() => {
      if (mapReadyRef.current) return;
      setMapInitTimedOut(true);
      console.error("[LocationPicker] Google Map initialization timed out on Android.");
    }, 5000);

    return () => {
      clearTimeout(timeout);
    };
  }, [visible, isAndroid, hasAndroidMapApiKey]);

  const resolveAddressFromCoords = useCallback(async (coords: LocationCoordinates) => {
    const requestId = ++reverseGeocodeRequestIdRef.current;

    if (!hasLocationPermission) {
      setSelectedAddress(`${coords.latitude.toFixed(6)}, ${coords.longitude.toFixed(6)}`);
      return;
    }

    try {
      const places = await Location.reverseGeocodeAsync({
        latitude: coords.latitude,
        longitude: coords.longitude,
      });
      if (requestId !== reverseGeocodeRequestIdRef.current) return;

      const place = places[0];
      const addressParts = [place?.name, place?.street, place?.district, place?.city, place?.region].filter(
        (p): p is string => typeof p === "string" && p.trim().length > 0,
      );

      if (addressParts.length > 0) {
        setSelectedAddress(addressParts.join(", "));
      } else {
        setSelectedAddress(`${coords.latitude.toFixed(6)}, ${coords.longitude.toFixed(6)}`);
      }
    } catch (error) {
      if (requestId !== reverseGeocodeRequestIdRef.current) return;
      console.error("[LocationPicker] Reverse geocode failed:", error);
      setSelectedAddress(`${coords.latitude.toFixed(6)}, ${coords.longitude.toFixed(6)}`);
    }
  }, [hasLocationPermission]);

  const handleRegionChangeComplete = useCallback(
    async (nextRegion: Region) => {
      setRegion(nextRegion);
      const centerCoords: LocationCoordinates = {
        latitude: nextRegion.latitude,
        longitude: nextRegion.longitude,
      };
      setSelectedCoords(centerCoords);
      await resolveAddressFromCoords(centerCoords);
    },
    [resolveAddressFromCoords],
  );

  const handleSearch = useCallback(async () => {
    if (!searchQuery.trim()) return;

    setIsSearching(true);
    try {
      const results = await Location.geocodeAsync(searchQuery);
      if (results.length > 0) {
        const result = results[0];
        const coords = {
          latitude: result.latitude,
          longitude: result.longitude,
        };

        setSelectedCoords(coords);
        setRegion({
          latitude: result.latitude,
          longitude: result.longitude,
          latitudeDelta: 0.01,
          longitudeDelta: 0.01,
        });

        mapRef.current?.animateToRegion(
          {
            latitude: result.latitude,
            longitude: result.longitude,
            latitudeDelta: 0.01,
            longitudeDelta: 0.01,
          },
          500,
        );
        await resolveAddressFromCoords(coords);
      }
    } catch (error) {
      console.error("[LocationPicker] Search failed:", error);
    } finally {
      setIsSearching(false);
    }
  }, [resolveAddressFromCoords, searchQuery]);

  const handleSave = useCallback(() => {
    if (!selectedCoords) return;

    const savedLocation: SavedLocation = {
      coordinates: selectedCoords,
      address: selectedAddress,
      timestamp: Date.now(),
    };

    setLocation(savedLocation);
    onClose();
  }, [selectedCoords, selectedAddress, setLocation, onClose]);

  const handleClose = useCallback(() => {
    setSearchQuery("");
    onClose();
  }, [onClose]);

  const canRenderMap = !isAndroid || (hasAndroidMapApiKey && !mapInitTimedOut);
  const mapUnavailableMessage = !hasAndroidMapApiKey
    ? "ไม่พบ Google Maps API key บน Android กรุณาตรวจสอบการตั้งค่า"
    : "แผนที่ไม่พร้อมใช้งานในขณะนี้ กรุณาปิดแล้วลองใหม่";

  const content = (
    <SafeAreaView style={[styles.container, { marginTop: topOffset }]} edges={["bottom"]}>
      <View style={styles.header}>
        <View style={styles.headerSide} />
        <Text style={styles.headerTitle}>สถานที่</Text>
        <Pressable style={styles.headerCloseButton} onPress={handleClose}>
          <Text style={styles.headerCloseText}>{isAndroid ? "ย้อนกลับ" : "ปิด"}</Text>
        </Pressable>
      </View>

      <View style={styles.searchContainer}>
        <View style={styles.searchInputWrapper}>
          <Search size={20} color="#6B7280" style={styles.searchIcon} />
          <TextInput
            style={styles.searchInput}
            placeholder="ค้นหาสถานที่จากชื่อ"
            placeholderTextColor="#9CA3AF"
            value={searchQuery}
            onChangeText={setSearchQuery}
            onSubmitEditing={handleSearch}
            returnKeyType="search"
          />
          {isSearching && <ActivityIndicator size="small" color="#3B82F6" />}
        </View>
      </View>

      <View style={styles.mapContainer}>
        {canRenderMap ? (
          <>
            <MapView
              ref={mapRef}
              style={styles.map}
              provider={PROVIDER_DEFAULT}
              region={region}
              onRegionChangeComplete={handleRegionChangeComplete}
              onMapReady={() => {
                mapReadyRef.current = true;
                setMapInitTimedOut(false);
              }}
              showsUserLocation={hasLocationPermission}
              showsMyLocationButton={hasLocationPermission}
            />
            <View pointerEvents="none" style={styles.centerMarkerOverlay}>
              <View style={styles.markerContainer}>
                <MapPin size={32} color="#EF4444" fill="#EF4444" />
              </View>
            </View>
          </>
        ) : (
          <View style={styles.mapUnavailableContainer}>
            <Text style={styles.mapUnavailableTitle}>Map unavailable</Text>
            <Text style={styles.mapUnavailableText}>{mapUnavailableMessage}</Text>
          </View>
        )}
      </View>

      {selectedAddress ? (
        <View style={styles.selectedAddressContainer}>
          <Text style={styles.selectedAddressLabel}>ที่อยู่ที่เลือก:</Text>
          <Text style={styles.selectedAddressText}>{selectedAddress}</Text>
        </View>
      ) : null}

      <View style={styles.footer}>
        <Button variant="default" size="lg" onPress={handleSave} disabled={!selectedCoords} style={styles.saveButton}>
          <Text style={styles.saveButtonText}>บันทึก</Text>
        </Button>
        <Button variant="secondary" size="lg" onPress={handleClose} style={styles.cancelButton}>
          ยกเลิก
        </Button>
      </View>
    </SafeAreaView>
  );

  if (presentation === "inline-overlay") {
    if (!visible) return null;

    return (
      <View style={styles.inlineOverlayRoot}>
        <Pressable style={styles.inlineBackdrop} onPress={handleClose} />
        {content}
      </View>
    );
  }

  return (
    <Modal visible={visible} animationType="slide" transparent onRequestClose={handleClose}>
      <View style={styles.backdrop}>{content}</View>
    </Modal>
  );
}
