import { createMMKV } from "react-native-mmkv";
import { create } from "zustand";
import { createJSONStorage, persist, StateStorage } from "zustand/middleware";

const MAX_HISTORY_ITEMS = 200;

// Initialize MMKV storage dedicated to call history
const historyStorage = createMMKV({
  id: "softphone-call-history",
});

// StateStorage adapter for zustand persist middleware
const mmkvStorage: StateStorage = {
  setItem: (name, value) => {
    historyStorage.set(name, value);
  },
  getItem: (name) => {
    const value = historyStorage.getString(name);
    return value ?? null;
  },
  removeItem: (name) => {
    historyStorage.remove(name);
  },
};

export type CallDirection = "outgoing" | "incoming";
export type CallResult = "answered" | "missed" | "declined" | "failed";

export interface CallHistoryEntry {
  id: string;
  phoneNumber: string;
  displayName: string | null;
  direction: CallDirection;
  result: CallResult;
  duration: number; // seconds
  timestamp: number; // Date.now()
}

interface CallHistoryState {
  entries: CallHistoryEntry[];
}

interface CallHistoryActions {
  addEntry: (entry: Omit<CallHistoryEntry, "id">) => void;
  removeEntry: (id: string) => void;
  clearHistory: () => void;
}

type CallHistoryStore = CallHistoryState & CallHistoryActions;

export const useCallHistoryStore = create<CallHistoryStore>()(
  persist(
    (set) => ({
      entries: [],

      addEntry: (entry) => {
        const newEntry: CallHistoryEntry = {
          ...entry,
          id: `call_${Date.now()}_${Math.random().toString(36).substr(2, 9)}`,
        };
        set((state) => ({
          entries: [newEntry, ...state.entries].slice(0, MAX_HISTORY_ITEMS),
        }));
        console.log("[CallHistoryStore] Entry added:", newEntry.phoneNumber, newEntry.direction, newEntry.result);
      },

      removeEntry: (id) => {
        set((state) => ({
          entries: state.entries.filter((e) => e.id !== id),
        }));
      },

      clearHistory: () => {
        set({ entries: [] });
        console.log("[CallHistoryStore] History cleared");
      },
    }),
    {
      name: "softphone-call-history-storage",
      storage: createJSONStorage(() => mmkvStorage),
      onRehydrateStorage: () => (state) => {
        if (state) {
          console.log("[CallHistoryStore] History hydrated from MMKV, entries:", state.entries.length);
        }
      },
    },
  ),
);
