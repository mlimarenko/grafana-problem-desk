import { describe, expect, it } from 'vitest';
import {
  activeFilterCount,
  buildProblemsRequest,
  closeProblem,
  defaultLimit,
  defaultOrder,
  defaultSort,
  mergedQueryString,
  pageCount,
  parseQuery,
  queryString,
  rangeWindow,
  resetFilters,
  selectProblem,
  serializeQuery,
  validateCustomRange,
  withView,
} from './query';

describe('workspace query', () => {
  it('uses the documented defaults', () => {
    const query = parseQuery('');
    expect(query.view).toBe('problems');
    expect(query.state).toBe('all');
    expect(query.sort).toBe(defaultSort);
    expect(query.order).toBe(defaultOrder);
    expect(query.limit).toBe(defaultLimit);
    expect(query.offset).toBe(0);
    expect(queryString(query)).toBe('');
    expect(parseQuery('?view=history').range).toBe('7d');
    expect(parseQuery('?view=recent').range).toBe('24h');
  });

  it('round-trips filters through the URL', () => {
    const query = parseQuery(
      '?view=recent&range=30d&state=active&severity=critical&host=worker&assignee=team-a&assignment=assigned&ack=acknowledged&search=db&labelKey=env&labelValue=prod&sort=duration&order=asc&limit=25&offset=50'
    );
    const restored = parseQuery(`?${serializeQuery(query)}`);
    expect(restored).toEqual(query);
  });

  it('preserves literal and empty label values in URLs and requests', () => {
    for (const value of [' prod ', '', '100%_']) {
      const params = new URLSearchParams({ labelKey: ' env ', labelValue: value });
      const query = parseQuery(`?${params}`);
      expect(buildProblemsRequest(query, new Date())).toMatchObject({ labelKey: ' env ', labelValue: value });
      expect(parseQuery(`?${serializeQuery(query)}`)).toEqual(query);
      expect(activeFilterCount(query)).toBe(1);
    }
  });

  it('does not send dates or state for the problems view', () => {
    const query = { ...parseQuery('?state=active&range=7d'), state: 'recovered' as const };
    const request = buildProblemsRequest(query, new Date('2026-11-10T12:00:00Z'));
    expect(request).toMatchObject({ view: 'problems', sort: 'severity', order: 'desc', limit: 50, offset: 0 });
    expect(request).not.toHaveProperty('from');
    expect(request).not.toHaveProperty('to');
    expect(request).not.toHaveProperty('state');
  });

  it('builds a rolling window for recent and history views', () => {
    const now = new Date('2026-11-10T12:00:00Z');
    const recent = buildProblemsRequest(parseQuery('?view=recent'), now);
    expect(recent.from).toBe('2026-11-09T12:00:00.000Z');
    expect(recent.to).toBe(now.toISOString());
    const history = buildProblemsRequest(parseQuery('?view=history&state=recovered'), now);
    expect(history.from).toBe('2026-11-03T12:00:00.000Z');
    expect(history.state).toBe('recovered');
  });

  it('uses a validated custom range and rejects invalid input', () => {
    expect(validateCustomRange('2026-11-01T00:00:00Z', '2026-11-02T00:00:00Z', 'history')).toBe('ok');
    expect(validateCustomRange('2026-11-02T00:00:00Z', '2026-11-01T00:00:00Z', 'history')).toBe('order');
    expect(validateCustomRange('not-a-date', '2026-11-02T00:00:00Z', 'history')).toBe('invalid');
    expect(validateCustomRange('2025-01-01T00:00:00Z', '2026-11-02T00:00:00Z', 'history')).toBe('too-long');
    const query = parseQuery(
      '?view=history&range=custom&from=2026-11-01T00:00:00Z&to=2026-11-02T00:00:00Z'
    );
    expect(query.range).toBe('custom');
    const request = buildProblemsRequest(query, new Date('2026-11-10T00:00:00Z'));
    expect(request.from).toBe('2026-11-01T00:00:00.000Z');
    expect(request.to).toBe('2026-11-02T00:00:00.000Z');
    expect(parseQuery('?view=history&range=custom&from=bad&to=2026-11-02T00:00:00Z').range).toBe('7d');
  });

  it('preserves incomplete and oversized label filters for backend rejection', () => {
    const onlyKey = parseQuery('?labelKey=env');
    expect(buildProblemsRequest(onlyKey, new Date())).toMatchObject({ labelKey: 'env' });
    expect(buildProblemsRequest(onlyKey, new Date())).not.toHaveProperty('labelValue');
    const pair = parseQuery('?labelKey=env&labelValue=prod');
    expect(buildProblemsRequest(pair, new Date())).toMatchObject({ labelKey: 'env', labelValue: 'prod' });
    const value = 'x'.repeat(1025);
    const invalid = parseQuery(`?${new URLSearchParams({ labelKey: 'env', labelValue: value })}`);
    expect(buildProblemsRequest(invalid, new Date())).toMatchObject({ labelKey: 'env', labelValue: value });
    expect(parseQuery(`?${serializeQuery(invalid)}`)).toEqual(invalid);
  });

  it('keeps the view range rules when switching tabs and resetting filters', () => {
    const problems = parseQuery('?severity=warning&offset=50');
    const history = withView(problems, 'history');
    expect(history.view).toBe('history');
    expect(history.range).toBe('7d');
    expect(history.offset).toBe(0);
    expect(history.severity).toBe('warning');
    const recent = withView(parseQuery('?range=30d'), 'recent');
    expect(recent.range).toBe('24h');
    const reset = resetFilters(history);
    expect(reset.severity).toBe('');
    expect(reset.range).toBe('7d');
    expect(reset.view).toBe('history');
  });

  it('counts active filters without counting sort, paging or a link', () => {
    const query = parseQuery('?view=history&state=active&severity=critical&host=db&labelKey=env&labelValue=prod&offset=25');
    expect(activeFilterCount(query)).toBe(4);
    expect(activeFilterCount(parseQuery(''))).toBe(0);
  });

  it('preserves unrelated query parameters and drops stale deep-link fields', () => {
    const query = selectProblem(parseQuery('?orgId=1&fingerprint=abc&startsAt=2026-11-09T00:00:00Z'), 'episode-1');
    const merged = new URLSearchParams(mergedQueryString('?orgId=1&fingerprint=abc&startsAt=2026-11-09T00:00:00Z', query));
    expect(merged.get('orgId')).toBe('1');
    expect(merged.get('problem')).toBe('episode-1');
    expect(merged.get('fingerprint')).toBeNull();
    expect(closeProblem(query).problem).toBe('');
  });

  it('keeps deep-link fields in the URL until the problem is selected', () => {
    const query = parseQuery(
      '?fingerprint=abc&startsAt=2026-11-09T00:00:00Z&ruleUID=rule-1&instance=host-1'
    );
    const params = serializeQuery(query);
    expect(params.get('fingerprint')).toBe('abc');
    expect(params.get('startsAt')).toBe('2026-11-09T00:00:00Z');
    expect(params.get('ruleUID')).toBe('rule-1');
    expect(params.get('instance')).toBe('host-1');
    expect(pageCount(51, 50)).toBe(2);
    expect(pageCount(0, 50)).toBe(1);
  });

  it('handles malformed pagination and range values with safe defaults', () => {
    const query = parseQuery('?limit=999&offset=-5&sort=unknown&order=sideways');
    expect(query.limit).toBe(50);
    expect(query.offset).toBe(0);
    expect(query.sort).toBe('severity');
    expect(query.order).toBe('desc');
    expect(rangeWindow(parseQuery('?view=recent'), new Date('2026-11-10T00:00:00Z'))).toBeDefined();
  });
});
