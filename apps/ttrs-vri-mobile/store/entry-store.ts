import { create } from "zustand";

export type EntryMode = "normal" | "emergency" | null;

interface EntryState {
  entryMode: EntryMode;
}

interface EntryActions {
  setEntryMode: (mode: EntryMode) => void;
}

type EntryStore = EntryState & EntryActions;

export const useEntryStore = create<EntryStore>()((set) => ({
  entryMode: null,
  setEntryMode: (mode) => set({ entryMode: mode }),
}));
