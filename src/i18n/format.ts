import type { Language } from './messages';

export type TimeZoneMode = 'local' | 'UTC';

const durationUnits: Record<Language, { day: string; hour: string; minute: string; second: string; lessThanMinute: string }> = {
  en: { day: 'd', hour: 'h', minute: 'm', second: 's', lessThanMinute: '<1m' },
  ru: { day: 'д', hour: 'ч', minute: 'мин', second: 'с', lessThanMinute: '<1 мин' },
};

const integer = new Intl.NumberFormat('en-US', { maximumFractionDigits: 0 });

export function formatDurationMs(ms: number, language: Language): string {
  if (!Number.isFinite(ms) || ms < 0) {
    return '—';
  }
  const units = durationUnits[language];
  const unit = (value: number, suffix: string) => `${integer.format(value)}${language === 'ru' ? ' ' : ''}${suffix}`;
  const totalSeconds = Math.floor(ms / 1000);
  if (totalSeconds < 60) {
    return units.lessThanMinute;
  }
  const totalMinutes = Math.floor(totalSeconds / 60);
  const days = Math.floor(totalMinutes / 1440);
  const hours = Math.floor((totalMinutes % 1440) / 60);
  const minutes = totalMinutes % 60;
  if (days > 0) {
    return hours > 0 ? `${unit(days, units.day)} ${unit(hours, units.hour)}` : unit(days, units.day);
  }
  if (hours > 0) {
    return minutes > 0 ? `${unit(hours, units.hour)} ${unit(minutes, units.minute)}` : unit(hours, units.hour);
  }
  return unit(minutes, units.minute);
}

function localeFor(language: Language): string {
  return language === 'ru' ? 'ru-RU' : 'en-GB';
}

export function formatDateTime(value: string, language: Language, timeZone: TimeZoneMode): string {
  if (!value) {
    return '—';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '—';
  }
  return new Intl.DateTimeFormat(localeFor(language), {
    year: 'numeric',
    month: 'short',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
    timeZone: timeZone === 'UTC' ? 'UTC' : undefined,
  }).format(date);
}

export function formatRelativeTime(value: string, now: number, language: Language): string {
  if (!value) {
    return '';
  }
  const then = new Date(value).getTime();
  if (Number.isNaN(then)) {
    return '';
  }
  const deltaSeconds = Math.round((then - now) / 1000);
  const relative = new Intl.RelativeTimeFormat(localeFor(language), { numeric: 'auto' });
  const absolute = Math.abs(deltaSeconds);
  if (absolute < 60) {
    return relative.format(deltaSeconds, 'second');
  }
  if (absolute < 3600) {
    return relative.format(Math.round(deltaSeconds / 60), 'minute');
  }
  if (absolute < 86400) {
    return relative.format(Math.round(deltaSeconds / 3600), 'hour');
  }
  return relative.format(Math.round(deltaSeconds / 86400), 'day');
}

export function toDateTimeLocalValue(value: string): string {
  if (!value) {
    return '';
  }
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) {
    return '';
  }
  const pad = (part: number) => String(part).padStart(2, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function dateTimeLocalToISO(value: string): string | undefined {
  if (!value) {
    return undefined;
  }
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? undefined : date.toISOString();
}

export function resolvedTimeZone(): string {
  try {
    return Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC';
  } catch {
    return 'UTC';
  }
}
