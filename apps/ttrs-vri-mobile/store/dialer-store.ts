import { createMMKV } from "react-native-mmkv";
import { create } from "zustand";
import { createJSONStorage, persist, StateStorage } from "zustand/middleware";

const MAX_DRAFT_LENGTH = 15;

// Initialize MMKV storage dedicated to dialer draft state
const dialerStorage = createMMKV({
  id: "softphone-dialer",
});

// StateStorage adapter for zustand persist middleware
const mmkvStorage: StateStorage = {
  setItem: (name, value) => {
    dialerStorage.set(name, value);
  },
  getItem: (name) => {
    const value = dialerStorage.getString(name);
    return value ?? null;
  },
  removeItem: (name) => {
    dialerStorage.remove(name);
  },
};

interface DialerState {
  draftNumber: string;
}

interface DialerActions {
  setDraftNumber: (value: string) => void;
  appendDigit: (digit: string) => void;
  backspace: () => void;
}

type DialerStore = DialerState & DialerActions;

export const useDialerStore = create<DialerStore>()(
  persist(
    (set, get) => ({
      draftNumber: "",

      setDraftNumber: (value) => {
        set({ draftNumber: value.slice(0, MAX_DRAFT_LENGTH) });
      },

      appendDigit: (digit) => {
        const current = get().draftNumber;
        if (current.length >= MAX_DRAFT_LENGTH) return;
        set({ draftNumber: (current + digit).slice(0, MAX_DRAFT_LENGTH) });
      },

      backspace: () => {
        const current = get().draftNumber;
        if (!current) return;
        set({ draftNumber: current.slice(0, -1) });
      },
    }),
    {
      name: "softphone-dialer-storage",
      storage: createJSONStorage(() => mmkvStorage),
    }
  )
);

