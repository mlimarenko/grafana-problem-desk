import { useState } from 'react';
import { pluginId } from '../model';

export function readPreference(key: string): string | undefined {
  try {
    return window.localStorage.getItem(`${pluginId}.${key}`) ?? undefined;
  } catch {
    return undefined;
  }
}

export function writePreference(key: string, value: string): void {
  try {
    window.localStorage.setItem(`${pluginId}.${key}`, value);
  } catch {
    // The selection still applies to the current page.
  }
}

export const drawerSections = ['workflow', 'actions', 'details'] as const;
export type ViewSection = 'summary' | (typeof drawerSections)[number];

export interface ViewPreferences {
  visible: ReadonlySet<ViewSection>;
  setVisible: (section: ViewSection, enabled: boolean) => void;
}

export function useViewPreferences(): ViewPreferences {
  const [visible, setSections] = useState(() => {
    const stored = readPreference('viewSections')?.split(',');
    return new Set<ViewSection>((['summary', ...drawerSections] as const).filter((section) => stored?.includes(section)));
  });
  return {
    visible,
    setVisible(section, enabled) {
      const next = new Set(visible);
      if (enabled) next.add(section);
      else next.delete(section);
      setSections(next);
      writePreference('viewSections', [...next].join(','));
    },
  };
}
