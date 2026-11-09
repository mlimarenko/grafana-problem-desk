import { describe, expect, it } from 'vitest';
import { Problem, describeError, problemTitle, safeURL, targetName } from './model';

const example: Problem = {
  id: 'episode-1', fingerprint: 'alert-1', startsAt: '2026-11-09T00:00:00Z', endsAt: '', status: 'active',
  labels: { instance: 'https://service.example.test', alertname: 'Endpoint unavailable' }, annotations: {},
  generatorURL: '', acknowledgedBy: '', acknowledgedAt: '', lastSeenAt: '2026-11-09T00:00:00Z',
};

describe('problem presentation', () => {
  it('keeps site targets identifiable without host metadata', () => {
    expect(targetName(example)).toBe('https://service.example.test');
    expect(problemTitle(example)).toBe('Endpoint unavailable');
  });
  it('prefers generic host metadata and the operator summary', () => {
    expect(targetName({ ...example, labels: { ...example.labels, host: 'worker-01' } })).toBe('worker-01');
    expect(problemTitle({ ...example, annotations: { summary: 'Database pool exhausted' } })).toBe('Database pool exhausted');
  });
  it('does not create executable or credential-bearing links from alert input', () => {
    for (const value of ['javascript:alert(1)', 'data:text/html,hello', 'file:///etc/passwd', '/relative', 'https://user:password@example.test']) {
      expect(safeURL(value)).toBeUndefined();
    }
    expect(safeURL('https://grafana.example.test/alerting')).toBe('https://grafana.example.test/alerting');
  });
  it('gives meaningful permission and missing-record errors', () => {
    expect(describeError({ status: 403 })).toContain('role');
    expect(describeError({ status: 404 })).toContain('organization');
  });
});
