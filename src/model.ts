export const pluginId = 'mlimarenko-problemdesk-app';
export interface Options { grafanaUrl?: string; pollIntervalSeconds?: number }
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
export interface Comment { id: string; body: string; author: string; createdAt: string }
export interface Event { id: string; kind: string; author: string; createdAt: string; body: string }
export interface Detail { problem: Problem; comments: Comment[]; events: Event[] }
export interface Listing { problems: Problem[]; total: number; source: { lastSuccess: string; error: string } }

export function targetName(problem: Problem): string {
  return problem.labels.host || problem.labels.hostname || problem.labels.instance || 'Unspecified target';
}

export function problemTitle(problem: Problem): string {
  return problem.annotations.summary || problem.labels.alertname || 'Unnamed problem';
}

export function safeURL(value: string): string | undefined {
  try {
    const url = new URL(value);
    return ['http:', 'https:'].includes(url.protocol) && !url.username && !url.password ? url.href : undefined;
  } catch {
    return undefined;
  }
}

export function describeError(error: unknown): string {
  const candidate = error as { status?: number; data?: { message?: string; error?: string } };
  if (candidate?.status === 403) { return 'Your Grafana role does not permit this action.'; }
  if (candidate?.status === 404) { return 'This problem is not available in your organization.'; }
  return candidate?.data?.message || candidate?.data?.error || 'The request failed. Check the plugin configuration and Grafana server logs.';
}
