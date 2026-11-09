import { getBackendSrv } from '@grafana/runtime';
import type { ResolveRequest } from './lib/deeplink';
import { buildProblemsRequest, type WorkspaceQuery } from './lib/query';
import {
  pluginId,
  type Comment,
  type Detail,
  type Facets,
  type Listing,
  type Problem,
  type SourceStatus,
  type Summary,
} from './model';

export const resource = `/api/plugins/${pluginId}/resources`;
const requestOptions = { showErrorAlert: false, showSuccessAlert: false };

export function emptySummary(): Summary {
  return { active: 0, recovered: 0, acknowledged: 0, unacknowledged: 0, unassigned: 0, critical: 0, warning: 0 };
}

function emptyFacets(): Facets {
  return { hosts: [], assignees: [], labelKeys: [] };
}

function numberValue(value: unknown): number {
  return typeof value === 'number' && Number.isFinite(value) ? value : 0;
}

function stringArray(value: unknown): string[] {
  return Array.isArray(value) ? value.filter((item): item is string => typeof item === 'string') : [];
}

function normalizeProblem(value: unknown): Problem | undefined {
  if (!value || typeof value !== 'object') {
    return undefined;
  }
  const item = value as Partial<Problem>;
  if (typeof item.id !== 'string' || (item.status !== 'active' && item.status !== 'recovered')) {
    return undefined;
  }
  return {
    id: item.id,
    fingerprint: typeof item.fingerprint === 'string' ? item.fingerprint : '',
    startsAt: typeof item.startsAt === 'string' ? item.startsAt : '',
    endsAt: typeof item.endsAt === 'string' ? item.endsAt : '',
    status: item.status,
    labels: item.labels && typeof item.labels === 'object' ? item.labels : {},
    annotations: item.annotations && typeof item.annotations === 'object' ? item.annotations : {},
    generatorURL: typeof item.generatorURL === 'string' ? item.generatorURL : '',
    acknowledgedBy: typeof item.acknowledgedBy === 'string' ? item.acknowledgedBy : '',
    acknowledgedAt: typeof item.acknowledgedAt === 'string' ? item.acknowledgedAt : '',
    assignee: typeof item.assignee === 'string' ? item.assignee : '',
    lastSeenAt: typeof item.lastSeenAt === 'string' ? item.lastSeenAt : '',
  };
}

export function normalizeListing(value: unknown): Listing {
  const raw = (value ?? {}) as Partial<Listing> & Record<string, unknown>;
  const source = (raw.source ?? {}) as Partial<SourceStatus>;
  const summary = (raw.summary ?? {}) as Partial<Summary>;
  const facets = (raw.facets ?? {}) as Partial<Facets>;
  const problems = Array.isArray(raw.problems)
    ? raw.problems.map(normalizeProblem).filter((problem): problem is Problem => Boolean(problem))
    : [];
  return {
    problems,
    total: numberValue(raw.total),
    source: {
      lastSuccess: typeof source.lastSuccess === 'string' ? source.lastSuccess : '',
      error: typeof source.error === 'string' ? source.error : '',
    },
    summary: {
      active: numberValue(summary.active),
      recovered: numberValue(summary.recovered),
      acknowledged: numberValue(summary.acknowledged),
      unacknowledged: numberValue(summary.unacknowledged),
      unassigned: numberValue(summary.unassigned),
      critical: numberValue(summary.critical),
      warning: numberValue(summary.warning),
    },
    facets: {
      hosts: stringArray(facets.hosts),
      assignees: stringArray(facets.assignees),
      labelKeys: stringArray(facets.labelKeys),
    },
  };
}

export async function fetchProblems(query: WorkspaceQuery, now: Date): Promise<Listing> {
  const response = await getBackendSrv().get<unknown>(`${resource}/problems`, buildProblemsRequest(query, now), undefined, requestOptions);
  return normalizeListing(response);
}

export async function fetchDetail(problemId: string): Promise<Detail> {
  const response = await getBackendSrv().get<Detail | undefined>(`${resource}/problems/${encodeURIComponent(problemId)}`, undefined, undefined, requestOptions);
  if (!response?.problem || typeof response.problem.id !== 'string') {
    throw new Error('invalid detail response');
  }
  return {
    problem: response.problem,
    comments: Array.isArray(response.comments) ? response.comments : [],
    events: Array.isArray(response.events) ? response.events : [],
  };
}

export async function resolveProblem(request: ResolveRequest): Promise<string> {
  const response = await getBackendSrv().get<{ problemId?: unknown }>(`${resource}/resolve`, request.params, undefined, requestOptions);
  if (typeof response?.problemId !== 'string' || !response.problemId) {
    throw new Error('invalid resolve response');
  }
  return response.problemId;
}

export function errorStatus(error: unknown): number | undefined {
  const candidate = error as { status?: number; statusCode?: number } | undefined;
  return candidate?.status ?? candidate?.statusCode;
}

export async function postComment(problemId: string, body: string): Promise<Comment> {
  return getBackendSrv().post<Comment>(`${resource}/problems/${encodeURIComponent(problemId)}/comments`, { body }, requestOptions);
}

export async function postAck(problemId: string): Promise<Problem> {
  return getBackendSrv().post<Problem>(`${resource}/problems/${encodeURIComponent(problemId)}/ack`, {}, requestOptions);
}

export async function postUnack(problemId: string): Promise<Problem> {
  return getBackendSrv().post<Problem>(`${resource}/problems/${encodeURIComponent(problemId)}/unack`, {}, requestOptions);
}

export async function postAssign(problemId: string, assignee: string): Promise<Problem> {
  return getBackendSrv().post<Problem>(`${resource}/problems/${encodeURIComponent(problemId)}/assign`, { assignee }, requestOptions);
}

export async function postSync(): Promise<void> {
  await getBackendSrv().post(`${resource}/sync`, {}, requestOptions);
}
