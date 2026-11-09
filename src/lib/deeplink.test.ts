import { describe, expect, it } from 'vitest';
import { parseQuery } from './query';
import { matchersFromFields, parseMatchers, resolveDeepLink } from './deeplink';

describe('episode deep links', () => {
  it('resolves an exact fingerprint and start time', () => {
    const result = resolveDeepLink(
      parseQuery('?fingerprint=abc123&startsAt=2026-11-09T00:00:00.000Z')
    );
    expect(result).toEqual({
      request: {
        mode: 'fingerprint',
        params: { fingerprint: 'abc123', startsAt: '2026-11-09T00:00:00.000Z' },
      },
    });
  });

  it('rejects an incomplete or malformed fingerprint link', () => {
    expect(resolveDeepLink(parseQuery('?fingerprint=abc123'))).toEqual({ error: 'fingerprint' });
    expect(resolveDeepLink(parseQuery('?fingerprint=abc123&startsAt=yesterday'))).toEqual({
      error: 'fingerprint',
    });
  });

  it('accepts a matcher JSON object with the required active-episode keys', () => {
    const matchers = JSON.stringify({
      __alert_rule_uid__: 'rule-1',
      instance: 'db-01',
      device: 'nvme0',
    });
    const result = resolveDeepLink(parseQuery(`?matchers=${encodeURIComponent(matchers)}`));
    expect(result).toEqual({
      request: {
        mode: 'matchers',
        params: {
          matchers: JSON.stringify({ __alert_rule_uid__: 'rule-1', instance: 'db-01', device: 'nvme0' }),
        },
      },
    });
  });

  it('rejects malformed matchers, unknown keys and missing required keys', () => {
    expect(parseMatchers('{')).toEqual({ valid: false });
    expect(parseMatchers(JSON.stringify({ __alert_rule_uid__: 'rule-1' }))).toEqual({ valid: false });
    expect(
      parseMatchers(JSON.stringify({ __alert_rule_uid__: 'rule-1', instance: 'db-01', customer: 'printum' }))
    ).toEqual({ valid: false });
    expect(
      resolveDeepLink(
        parseQuery(`?matchers=${encodeURIComponent(JSON.stringify({ __alert_rule_uid__: 'rule-1', instance: 'db-01', customer: 'printum' }))}`)
      )
    ).toEqual({ error: 'matchers' });
  });

  it('turns dashboard row fields into matchers without empty discriminators', () => {
    const query = parseQuery(
      '?ruleUID=rule-9&instance=host-9&device=nvme1&mountpoint=&name=data&datname='
    );
    const built = matchersFromFields(query);
    expect(built.valid).toBe(true);
    expect(built.matchers).toEqual({
      __alert_rule_uid__: 'rule-9',
      instance: 'host-9',
      device: 'nvme1',
      name: 'data',
    });
    expect(resolveDeepLink(query)).toEqual({
      request: {
        mode: 'matchers',
        params: {
          matchers: JSON.stringify({
            __alert_rule_uid__: 'rule-9',
            instance: 'host-9',
            device: 'nvme1',
            name: 'data',
          }),
        },
      },
    });
  });

  it('preserves exact dashboard field values without trimming identity', () => {
    const query = parseQuery('?ruleUID=rule%201&instance=host%201');
    expect(query.ruleUID).toBe('rule 1');
    expect(query.instance).toBe('host 1');
    expect(matchersFromFields(query).matchers).toEqual({
      __alert_rule_uid__: 'rule 1',
      instance: 'host 1',
    });
  });

  it('rejects a partial dashboard row link instead of guessing a row', () => {
    expect(resolveDeepLink(parseQuery('?ruleUID=rule-9'))).toEqual({ error: 'fields' });
    expect(resolveDeepLink(parseQuery('?device=nvme1'))).toEqual({ error: 'fields' });
    expect(resolveDeepLink(parseQuery(''))).toBeUndefined();
  });

  it('rejects oversized identity fields before resolving a narrower link', () => {
    const fields: Array<[string, number, string]> = [
      ['device', 1025, 'fields'], ['mountpoint', 1025, 'fields'], ['name', 1025, 'fields'],
      ['datname', 1025, 'fields'], ['ruleUID', 257, 'fields'], ['instance', 1025, 'fields'],
      ['fingerprint', 1025, 'fingerprint'], ['startsAt', 65, 'fingerprint'], ['matchers', 16385, 'matchers'],
    ];
    for (const [field, size, error] of fields) {
      const params = new URLSearchParams({ ruleUID: 'r', instance: 'h', [field]: 'x'.repeat(size) });
      expect(resolveDeepLink(parseQuery(`?${params}`)), field).toEqual({ error });
    }
    const multibyte = new URLSearchParams({ ruleUID: 'r', instance: 'h', device: 'é'.repeat(513) });
    expect(resolveDeepLink(parseQuery(`?${multibyte}`))).toEqual({ error: 'fields' });
  });

  it('keeps an explicit canonical problem selection ahead of link resolution', () => {
    const query = parseQuery('?problem=episode-1&fingerprint=abc&startsAt=2026-11-09T00:00:00Z');
    expect(resolveDeepLink(query)).toBeUndefined();
  });
});
