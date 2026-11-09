import React, { createContext, useCallback, useContext, useEffect, useMemo, useState, type PropsWithChildren } from 'react';
import { config } from '@grafana/runtime';
import { pluginId, type ErrorKind } from '../model';
import { dictionaries, resolveLanguage, type Language, type Messages } from './messages';
import { formatDateTime, formatDurationMs, formatRelativeTime, resolvedTimeZone, type TimeZoneMode } from './format';

const languageStorageKey = `${pluginId}.language`;
const timeZoneStorageKey = `${pluginId}.timeZone`;

function readStorage(key: string): string | undefined {
  try {
    return window.localStorage.getItem(key) ?? undefined;
  } catch {
    return undefined;
  }
}

function writeStorage(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // The current page keeps the selection when storage is unavailable.
  }
}

export function detectLanguage(): Language {
  const stored = readStorage(languageStorageKey);
  if (stored === 'en' || stored === 'ru') {
    return stored;
  }
  const browserLanguages = typeof navigator === 'undefined' ? [] : [...(navigator.languages ?? []), navigator.language];
  return resolveLanguage([config.bootData?.user?.language, ...browserLanguages]);
}

export function detectTimeZone(): TimeZoneMode {
  return readStorage(timeZoneStorageKey) === 'UTC' ? 'UTC' : 'local';
}

export function errorText(kind: ErrorKind, messages: Messages): string {
  switch (kind) {
    case 'forbidden':
      return messages.errors.forbidden;
    case 'missing':
      return messages.errors.missing;
    case 'invalid':
      return messages.errors.invalid;
    default:
      return messages.errors.unavailable;
  }
}

interface I18nValue {
  language: Language;
  messages: Messages;
  setLanguage: (language: Language) => void;
  timeZone: TimeZoneMode;
  setTimeZone: (timeZone: TimeZoneMode) => void;
  zoneLabel: string;
  formatDate: (value: string) => string;
  formatDuration: (milliseconds: number) => string;
  formatRelative: (value: string, now?: number) => string;
}

const I18nContext = createContext<I18nValue | undefined>(undefined);

export function I18nProvider({ children }: PropsWithChildren) {
  const [language, setLanguageState] = useState<Language>(() => detectLanguage());
  const [timeZone, setTimeZoneState] = useState<TimeZoneMode>(() => detectTimeZone());

  const setLanguage = useCallback((next: Language) => {
    setLanguageState(next);
    writeStorage(languageStorageKey, next);
  }, []);

  const setTimeZone = useCallback((next: TimeZoneMode) => {
    setTimeZoneState(next);
    writeStorage(timeZoneStorageKey, next);
  }, []);

  useEffect(() => {
    document.documentElement.lang = language;
  }, [language]);

  const messages = dictionaries[language];
  const zoneLabel = timeZone === 'UTC' ? messages.app.utc : messages.app.localZone(resolvedTimeZone());

  const value = useMemo<I18nValue>(
    () => ({
      language,
      messages,
      setLanguage,
      timeZone,
      setTimeZone,
      zoneLabel,
      formatDate: (input) => formatDateTime(input, language, timeZone),
      formatDuration: (milliseconds) => formatDurationMs(milliseconds, language),
      formatRelative: (input, now = Date.now()) => formatRelativeTime(input, now, language),
    }),
    [language, messages, setLanguage, timeZone, setTimeZone, zoneLabel]
  );

  return <I18nContext.Provider value={value}>{children}</I18nContext.Provider>;
}

export function useI18n(): I18nValue {
  const value = useContext(I18nContext);
  if (!value) {
    throw new Error('useI18n must be used inside I18nProvider');
  }
  return value;
}

export { dictionaries, resolveLanguage };
export type { Language, Messages };
