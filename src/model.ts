import type { GrafanaTheme2 } from '@grafana/data';
import { dictionaries } from './i18n/messages';

export const pluginId = 'mlimarenko-problemdesk-app';

export interface Options {
  grafanaUrl?: string;
  pollIntervalSeconds?: number;
}

export interface Problem {
  id: string;
  levelId: string;
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
  byLevel: Record<string, number>;
}

export type LevelTone = 'red' | 'orange' | 'yellow' | 'blue' | 'green' | 'purple' | 'grey';
export interface LocalizedName { ru: string; en: string }
export interface Level { id: string; rank: number; tone: LevelTone; name: LocalizedName; aliases: string[] }
export interface Requires { ack?: boolean; owner?: boolean; comment?: boolean; sourceRecovered?: boolean }
export type MinimumRole = 'editor' | 'admin';
export interface PolicyState { id: string; rank: number; name: LocalizedName; operatorComplete?: boolean }
export interface PolicyTransition { from: string; to: string; minimumRole: MinimumRole; requires: Requires }
export interface PolicyWorkflow { id: string; name: LocalizedName; initialState: string; minimumRole: MinimumRole; startRequires: Requires; states: PolicyState[]; transitions: PolicyTransition[] }
export interface PolicyRoute { match: { labels: Record<string, string>; level?: string }; workflow: string }
export type PolicyRange = '1h' | '24h' | '7d' | '30d';
export type PolicyColumn = 'level' | 'name' | 'status' | 'started' | 'duration' | 'owner' | 'ack';
export interface PolicyViews { recent: PolicyRange; history: PolicyRange; columns: PolicyColumn[] }
export interface PolicyDoc { schemaVersion: 1; severityKey: string; levels: Level[]; workflows: PolicyWorkflow[]; routing: PolicyRoute[]; defaultWorkflow: string | null; views: PolicyViews; source: { recoveredTrustMinutes: number } }
export interface PolicyResponse { revision: number; doc: PolicyDoc; defaultsApplied: boolean; occupancy: Array<{ workflowId: string; stateId: string; count: number }> }
export interface PolicyIssue { path: string; code: string; message: string }
export interface PolicyOccupancy { workflowId: string; stateId: string; count: number }
export interface PolicyPreviewSample { labels: Record<string, string>; acknowledged?: boolean; assignee?: string; status?: 'active' | 'recovered'; comment?: string }
export interface PolicyPreview { valid: boolean; issues: PolicyIssue[]; occupancyViolations: PolicyOccupancy[]; samples: Array<{ levelId: string; levelMatched: boolean; workflowId: string | null; canStart: boolean; missingStartRequires: string[] }> }
export interface PolicyRevision { revision: number; createdAt: string; createdBy: string; action: string }
export interface WorkflowEdge { to: string; allowed: boolean; requiredRole: MinimumRole; missing: string[] }
export type WorkflowStatus = { tracked: false; policyRevision: number; availableWorkflows: Array<{ id: string; name: LocalizedName; minimumRole: MinimumRole }>; suggestedWorkflow?: string } | { tracked: true; workflowId: string; stateId: string; revisionUsed: number; retired: boolean; sourceConflict: boolean; source: { status: string; lastSuccess: string; error: string; trusted: boolean; stale: boolean }; edges: WorkflowEdge[] };

export function normalizeMinimumRole(value: unknown): MinimumRole | undefined {
  if (value === undefined || value === '' || value === 'editor') return 'editor';
  return value === 'admin' ? 'admin' : undefined;
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
  levels: Level[];
  severityKey: string;
  policyRevision: number;
}

export type ErrorKind = 'forbidden' | 'missing' | 'invalid' | 'unavailable';

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

export function levelDisplay(problem: Problem, levels: Level[], severityKey: string, language: 'ru' | 'en'): { label: string; tone: LevelTone } {
  const level = levels.find((item) => item.id === problem.levelId);
  const raw = problem.labels[severityKey] ?? '';
  if (!level) {
    return { label: raw || dictionaries[language].summary.retired(problem.levelId), tone: 'grey' };
  }
  return { label: raw && !level.aliases.some((alias) => alias.toLowerCase() === raw.toLowerCase()) ? raw : level.name[language], tone: level.tone };
}

export function compareLevels(left: Level, right: Level): number {
  return right.rank - left.rank || (left.id < right.id ? -1 : left.id > right.id ? 1 : 0);
}

export function levelColor(theme: GrafanaTheme2, tone: LevelTone): string {
  return tone === 'grey' ? theme.colors.text.secondary : theme.visualization.getColorByName(tone);
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
