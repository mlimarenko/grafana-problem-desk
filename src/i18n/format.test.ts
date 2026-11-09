import { describe, expect, it } from 'vitest';
import {
  dateTimeLocalToISO,
  formatDateTime,
  formatDurationMs,
  formatRelativeTime,
  toDateTimeLocalValue,
} from '../i18n/format';

describe('localized formatting', () => {
  it('formats durations in both languages', () => {
    expect(formatDurationMs(0, 'en')).toBe('<1m');
    expect(formatDurationMs(65 * 1000, 'en')).toBe('1m');
    expect(formatDurationMs((2 * 60 + 5) * 60 * 1000, 'en')).toBe('2h 5m');
    expect(formatDurationMs((26 * 60) * 60 * 1000, 'en')).toBe('1d 2h');
    expect(formatDurationMs(65 * 1000, 'ru')).toBe('1 мин');
    expect(formatDurationMs((2 * 60 + 5) * 60 * 1000, 'ru')).toBe('2 ч 5 мин');
    expect(formatDurationMs(Number.NaN, 'en')).toBe('—');
  });

  it('round-trips datetime-local values through ISO', () => {
    const iso = dateTimeLocalToISO('2026-11-09T12:30');
    expect(iso).toBeDefined();
    expect(toDateTimeLocalValue(iso ?? '')).toBe('2026-11-09T12:30');
    expect(dateTimeLocalToISO('not-a-date')).toBeUndefined();
    expect(toDateTimeLocalValue('not-a-date')).toBe('');
  });

  it('formats dates in the requested zone and labels invalid values', () => {
    expect(formatDateTime('2026-11-09T12:00:00Z', 'en', 'UTC')).toContain('12:00');
    expect(formatDateTime('not-a-date', 'ru', 'local')).toBe('—');
    expect(formatDateTime('', 'en', 'local')).toBe('—');
  });

  it('formats relative source timestamps', () => {
    const now = Date.parse('2026-11-09T12:05:00Z');
    expect(formatRelativeTime('2026-11-09T12:00:00Z', now, 'en')).toContain('5');
    expect(formatRelativeTime('', now, 'en')).toBe('');
  });
});
