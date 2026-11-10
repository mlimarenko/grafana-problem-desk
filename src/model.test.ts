import { describe, expect, it } from 'vitest';
import {
  assignmentTarget,
  classifyError,
  elapsedMs,
  mergedActivity,
  problemTitle,
  safeURL,
  levelDisplay,
  compareLevels,
  targetMetadata,
  targetName,
  type Problem,
  type Level,
} from './model';

const example: Problem = {
  id: 'episode-1',
  levelId: 'warning',
  fingerprint: 'alert-1',
  startsAt: '2026-11-09T00:00:00Z',
  endsAt: '',
  status: 'active',
  labels: { instance: 'https://service.example.test', alertname: 'Endpoint unavailable' },
  annotations: {},
  generatorURL: '',
  acknowledgedBy: '',
  acknowledgedAt: '',
  lastSeenAt: '2026-11-09T00:00:00Z',
};

describe('problem presentation', () => {
  it('uses the generic host metadata before the instance address', () => {
    expect(targetName(example)).toBe('https://service.example.test');
    expect(targetName({ ...example, labels: { ...example.labels, hostname: 'worker-02' } })).toBe('worker-02');
    expect(targetName({ ...example, labels: { ...example.labels, host: 'worker-01' } })).toBe('worker-01');
    expect(targetName({ ...example, labels: { ...example.labels, piping_host_name: 'pipe-01', host: 'worker-01' } })).toBe(
      'worker-01'
    );
  });

  it('keeps address and job as secondary target metadata', () => {
    expect(targetMetadata({ ...example, labels: { address: '10.0.0.5', job: 'node-exporter' } })).toEqual([
      '10.0.0.5',
      'node-exporter',
    ]);
  });

  it('prefers the operator summary over the rule name', () => {
    expect(problemTitle(example)).toBe('Endpoint unavailable');
    expect(problemTitle({ ...example, annotations: { summary: 'Database pool exhausted' } })).toBe('Database pool exhausted');
  });

  it('uses level aliases literally and preserves unmatched raw text', () => {
    const levels: Level[] = [{ id: 'warning', rank: 3, tone: 'yellow', name: { en: 'Warning', ru: 'Внимание' }, aliases: ['average', ' spaced '] }];
    const display = (raw: string, id = 'warning') => levelDisplay({ ...example, levelId: id, labels: { priority: raw } }, levels, 'priority', 'en');
    expect(display('AVERAGE')).toEqual({ label: 'Warning', tone: 'yellow' });
    expect(display(' SPACED ')).toEqual({ label: 'Warning', tone: 'yellow' });
    expect(display('average ')).toEqual({ label: 'average ', tone: 'yellow' });
    expect(display('', 'removed')).toEqual({ label: 'retired: removed', tone: 'grey' });
    expect(display('custom', 'removed')).toEqual({ label: 'custom', tone: 'grey' });
    const a = { ...levels[0], id: 'a' };
    const z = { ...levels[0], id: 'z' };
    expect([z, a].sort(compareLevels).map((level) => level.id)).toEqual(['a', 'z']);
  });

  it('calculates the age or recovery duration without negative values', () => {
    expect(elapsedMs(example, Date.parse('2026-11-09T03:00:00Z'))).toBe(3 * 60 * 60 * 1000);
    expect(
      elapsedMs({ ...example, status: 'recovered', endsAt: '2026-11-09T01:00:00Z' }, Date.parse('2026-11-10T00:00:00Z'))
    ).toBe(60 * 60 * 1000);
    expect(elapsedMs({ ...example, startsAt: 'not-a-time' }, Date.now())).toBeUndefined();
  });

  it('merges comments and audit events newest first and keeps action identity', () => {
    const detail = {
      comments: [{ id: 'c1', body: 'Checking', author: 'anna', createdAt: '2026-11-09T01:00:00Z' }],
      events: [
        { id: 'e1', kind: 'ack', author: 'ivan', createdAt: '2026-11-09T02:00:00Z', body: 'Problem acknowledged' },
        { id: 'e2', kind: 'assign', author: 'ivan', createdAt: '2026-11-09T03:00:00Z', body: 'Assigned to team-a' },
      ],
    };
    const activity = mergedActivity(detail);
    expect(activity.map((item) => item.id)).toEqual(['event:e2', 'event:e1', 'comment:c1']);
    expect(activity[0].kind).toBe('assign');
    expect(assignmentTarget(activity[0].body ?? '')).toBe('team-a');
    expect(assignmentTarget('Assignment cleared')).toBeUndefined();
  });

  it('does not create executable or credential-bearing links from alert input', () => {
    for (const value of ['javascript:alert(1)', 'data:text/html,hello', 'file:///etc/passwd', '/relative', 'example.test']) {
      expect(safeURL(value)).toBeUndefined();
    }
    expect(safeURL('https://grafana.example.test/alerting')).toBe('https://grafana.example.test/alerting');
  });

  it('classifies transport errors without exposing backend text', () => {
    expect(classifyError({ status: 403 })).toBe('forbidden');
    expect(classifyError({ statusCode: 404 })).toBe('missing');
    expect(classifyError({ status: 409 })).toBe('invalid');
    expect(classifyError(new Error('socket closed'))).toBe('unavailable');
  });
});
