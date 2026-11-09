import { describe, expect, it } from 'vitest';
import { dictionaries, resolveLanguage } from './messages';

function keys(value: object, prefix = ''): string[] {
  return Object.entries(value).flatMap(([key, child]) => {
    const path = prefix ? `${prefix}.${key}` : key;
    return child && typeof child === 'object' ? keys(child, path) : [path];
  });
}

describe('translation dictionary', () => {
  it('keeps English and Russian key sets identical', () => {
    expect(keys(dictionaries.ru).sort()).toEqual(keys(dictionaries.en).sort());
  });

  it('resolves Russian browser and Grafana language tags', () => {
    expect(resolveLanguage(['ru-RU'])).toBe('ru');
    expect(resolveLanguage(['en-US', 'ru'])).toBe('ru');
    expect(resolveLanguage(['en-GB'])).toBe('en');
    expect(resolveLanguage([undefined, null, ''])).toBe('en');
  });

  it('pluralizes episode counts in both languages', () => {
    expect(dictionaries.en.summary.matching(1)).toBe('1 episode');
    expect(dictionaries.en.summary.matching(3)).toBe('3 episodes');
    expect(dictionaries.ru.summary.matching(1)).toBe('1 эпизод');
    expect(dictionaries.ru.summary.matching(2)).toBe('2 эпизода');
    expect(dictionaries.ru.summary.matching(5)).toBe('5 эпизодов');
    expect(dictionaries.ru.summary.matching(11)).toBe('11 эпизодов');
  });
});
