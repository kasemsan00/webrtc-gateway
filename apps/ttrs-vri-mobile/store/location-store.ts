import { create } from "zustand";

export interface LocationCoordinates {
  latitude: number;
  longitude: number;
}

export interface SavedLocation {
  coordinates: LocationCoordinates;
  address: string;
  timestamp: number;
}

interface LocationState {
  currentLocation: SavedLocation | null;
  isManualLocation: boolean;
}

interface LocationActions {
  setLocation: (location: SavedLocation) => void;
  clearLocation: () => void;
}

type LocationStore = LocationState & LocationActions;

export const useLocationStore = create<LocationStore>()((set) => ({
  currentLocation: null,
  isManualLocation: false,

  setLocation: (location: SavedLocation) => {
    set({
      currentLocation: location,
      isManualLocation: true,
    });
  },

  clearLocation: () => {
    set({
      currentLocation: null,
      isManualLocation: false,
    });
  },
}));
