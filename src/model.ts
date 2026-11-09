export const pluginId = 'mlimarenko-problemdesk-app';

export interface Options {
  grafanaUrl?: string;
  pollIntervalSeconds?: number;
}

export interface Problem {
  id: string;
  fingerprint: string;
  startsAt: string;
  endsAt: string;
  status: 'active' | 'recovered';
  labels: Record<string, string>;
  annotations: Record<string, string>;
  generatorURL: string;
  acknowledgedBy: string;
  acknowledgedAt: string;
  assignee?: string;
  lastSeenAt: string;
}

export interface Comment {
  id: string;
  body: string;
  author: string;
  createdAt: string;
}

export interface Event {
  id: string;
  kind: string;
  author: string;
  createdAt: string;
  body: string;
}

export interface Detail {
  problem: Problem;
  comments: Comment[];
  events: Event[];
}

export interface Summary {
  active: number;
  recovered: number;
  acknowledged: number;
  unacknowledged: number;
  unassigned: number;
  critical: number;
  warning: number;
}

export interface Facets {
  hosts: string[];
  assignees: string[];
  labelKeys: string[];
}

export interface SourceStatus {
  lastSuccess: string;
  error: string;
}

export interface Listing {
  problems: Problem[];
  total: number;
  source: SourceStatus;
  summary: Summary;
  facets: Facets;
}

export type ErrorKind = 'forbidden' | 'missing' | 'invalid' | 'unavailable';

export type SeverityTone = 'critical' | 'warning' | 'info' | 'unknown';

export interface ActivityItem {
  id: string;
  at: string;
  author: string;
  kind: 'comment' | 'ack' | 'unack' | 'assign' | 'event';
  eventKind?: string;
  body?: string;
}

const hostLabelKeys = ['host', 'hostname', 'piping_host_name'] as const;

export function targetName(problem: Problem): string {
  for (const key of hostLabelKeys) {
    const value = problem.labels[key]?.trim();
    if (value) {
      return value;
    }
  }
  return problem.labels.instance?.trim() || '';
}

export function targetMetadata(problem: Problem): string[] {
  return [problem.labels.address?.trim(), problem.labels.job?.trim()].filter((value): value is string => Boolean(value));
}

export function problemTitle(problem: Problem): string {
  return problem.annotations.summary?.trim() || problem.labels.alertname?.trim() || '';
}

export function isAcknowledged(problem: Problem): boolean {
  return Boolean(problem.acknowledgedBy.trim());
}

export function severityTone(severity: string | undefined): SeverityTone {
  switch ((severity ?? '').trim().toLowerCase()) {
    case 'critical':
    case 'disaster':
    case 'high':
      return 'critical';
    case 'warning':
    case 'average':
      return 'warning';
    case 'info':
    case 'information':
      return 'info';
    default:
      return 'unknown';
  }
}

export function severityRank(severity: string | undefined): number {
  switch (severityTone(severity)) {
    case 'critical':
      return 3;
    case 'warning':
      return 2;
    case 'info':
      return 1;
    default:
      return 0;
  }
}

export function elapsedMs(problem: Problem, now: number): number | undefined {
  const start = new Date(problem.startsAt).getTime();
  if (Number.isNaN(start)) {
    return undefined;
  }
  const end = problem.endsAt ? new Date(problem.endsAt).getTime() : now;
  if (Number.isNaN(end)) {
    return undefined;
  }
  return Math.max(0, end - start);
}

export function assignmentTarget(body: string): string | undefined {
  const match = /^Assigned to (.+)$/.exec(body.trim());
  return match?.[1]?.trim() || undefined;
}

export function mergedActivity(detail: Pick<Detail, 'comments' | 'events'>): ActivityItem[] {
  const comments: ActivityItem[] = (detail.comments ?? []).map((comment) => ({
    id: `comment:${comment.id}`,
    at: comment.createdAt,
    author: comment.author,
    kind: 'comment',
    body: comment.body,
  }));
  const events: ActivityItem[] = (detail.events ?? []).map((event) => {
    const kind = event.kind === 'ack' || event.kind === 'unack' || event.kind === 'assign' ? event.kind : 'event';
    return {
      id: `event:${event.id}`,
      at: event.createdAt,
      author: event.author,
      kind,
      eventKind: event.kind,
      body: event.body,
    };
  });
  const time = (value: string) => {
    const parsed = new Date(value).getTime();
    return Number.isNaN(parsed) ? 0 : parsed;
  };
  return [...comments, ...events].sort((left, right) => time(right.at) - time(left.at) || right.id.localeCompare(left.id));
}

export function safeURL(value: string): string | undefined {
  try {
    const url = new URL(value);
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export function classifyError(error: unknown): ErrorKind {
  const candidate = error as { status?: number; statusCode?: number; data?: { status?: number } } | undefined;
  const status = candidate?.status ?? candidate?.statusCode ?? candidate?.data?.status;
  if (status === 401 || status === 403) {
    return 'forbidden';
  }
  if (status === 404) {
    return 'missing';
  }
  if (status === 400 || status === 409 || status === 413 || status === 422) {
    return 'invalid';
  }
  return 'unavailable';
}
