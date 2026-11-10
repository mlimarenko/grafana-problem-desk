import { getBackendSrv } from '@grafana/runtime';
import {
  ParticipantsError,
  parseCatalog,
  participantsFromRows,
  type AssigneeCatalog,
  type AssigneeMember,
} from './lib/assignees';
import type { ResolveRequest } from './lib/deeplink';
import { buildProblemsRequest, type WorkspaceQuery } from './lib/query';
import {
  pluginId,
  normalizeMinimumRole,
  type Comment,
  type Detail,
  type Facets,
  type Listing,
  type Level,
  type Problem,
  type PolicyResponse,
  type PolicyDoc,
  type PolicyIssue,
  type PolicyPreview,
  type PolicyPreviewSample,
  type PolicyRevision,
  type WorkflowStatus,
  type SourceStatus,
  type Summary,
} from './model';

export const resource = `/api/plugins/${pluginId}/resources`;
const requestOptions = { showErrorAlert: false, showSuccessAlert: false };

export function emptySummary(): Summary {
  return { active: 0, recovered: 0, acknowledged: 0, unacknowledged: 0, unassigned: 0, byLevel: Object.create(null) };
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

function stringRecord(value: unknown): Record<string, string> {
  const result: Record<string, string> = Object.create(null);
  if (value && typeof value === 'object' && !Array.isArray(value)) {
    for (const [key, item] of Object.entries(value)) if (typeof item === 'string') result[key] = item;
  }
  return result;
}

function levelsValue(value: unknown): Level[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is Level => item && typeof item === 'object' && typeof item.id === 'string' &&
    Number.isInteger(item.rank) && ['red', 'orange', 'yellow', 'blue', 'green', 'purple', 'grey'].includes(item.tone) &&
    item.name && typeof item.name.ru === 'string' && typeof item.name.en === 'string' &&
    Array.isArray(item.aliases) && item.aliases.every((alias: unknown) => typeof alias === 'string'));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value));
}

function validName(value: unknown): boolean {
  return isRecord(value) && typeof value.en === 'string' && typeof value.ru === 'string';
}

function validRequires(value: unknown): boolean {
  return isRecord(value) && Object.entries(value).every(([key, flag]) => ['ack', 'owner', 'comment', 'sourceRecovered'].includes(key) && typeof flag === 'boolean');
}

function validPolicyDoc(value: unknown): boolean {
  if (!isRecord(value) || value.schemaVersion !== 1 || typeof value.severityKey !== 'string' ||
      !Array.isArray(value.levels) || levelsValue(value.levels).length !== value.levels.length ||
      !Array.isArray(value.workflows) || !Array.isArray(value.routing) ||
      !(value.defaultWorkflow === null || typeof value.defaultWorkflow === 'string') || !isRecord(value.views) ||
      !['1h', '24h', '7d', '30d'].includes(String(value.views.recent)) ||
      !['1h', '24h', '7d', '30d'].includes(String(value.views.history)) || !Array.isArray(value.views.columns) ||
      value.views.columns.length === 0 || value.views.columns.some((column) => !['level', 'name', 'status', 'started', 'duration', 'owner', 'ack'].includes(column)) ||
      !isRecord(value.source) || !Number.isInteger(value.source.recoveredTrustMinutes)) return false;
  return value.workflows.every((workflow) => isRecord(workflow) && typeof workflow.id === 'string' && validName(workflow.name) && normalizeMinimumRole(workflow.minimumRole) !== undefined &&
    typeof workflow.initialState === 'string' && validRequires(workflow.startRequires) && Array.isArray(workflow.states) &&
    workflow.states.every((state) => isRecord(state) && typeof state.id === 'string' && Number.isInteger(state.rank) && validName(state.name) &&
      (state.operatorComplete === undefined || typeof state.operatorComplete === 'boolean')) && Array.isArray(workflow.transitions) &&
    workflow.transitions.every((transition) => isRecord(transition) && typeof transition.from === 'string' && typeof transition.to === 'string' && normalizeMinimumRole(transition.minimumRole) !== undefined && validRequires(transition.requires))) &&
    value.routing.every((route) => isRecord(route) && typeof route.workflow === 'string' && isRecord(route.match) &&
      isRecord(route.match.labels) && Object.values(route.match.labels).every((label) => typeof label === 'string') &&
      (route.match.level === undefined || typeof route.match.level === 'string'));
}

function normalizePolicyRoles(doc: PolicyDoc): PolicyDoc {
  const role = (value: unknown) => {
    const normalized = normalizeMinimumRole(value);
    if (!normalized) throw new Error('invalid policy role');
    return normalized;
  };
  return { ...doc, workflows: doc.workflows.map((workflow) => ({
    ...workflow, minimumRole: role(workflow.minimumRole),
    transitions: workflow.transitions.map((transition) => ({ ...transition, minimumRole: role(transition.minimumRole) })),
  })) };
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
    levelId: typeof item.levelId === 'string' ? item.levelId : '',
    fingerprint: typeof item.fingerprint === 'string' ? item.fingerprint : '',
    startsAt: typeof item.startsAt === 'string' ? item.startsAt : '',
    endsAt: typeof item.endsAt === 'string' ? item.endsAt : '',
    status: item.status,
    labels: stringRecord(item.labels),
    annotations: stringRecord(item.annotations),
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
  const byLevel: Record<string, number> = Object.create(null);
  if (summary.byLevel && typeof summary.byLevel === 'object' && !Array.isArray(summary.byLevel)) {
    for (const [id, count] of Object.entries(summary.byLevel)) byLevel[id] = numberValue(count);
  }
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
      byLevel,
    },
    facets: {
      hosts: stringArray(facets.hosts),
      assignees: stringArray(facets.assignees),
      labelKeys: stringArray(facets.labelKeys),
    },
    levels: levelsValue(raw.levels),
    severityKey: typeof raw.severityKey === 'string' ? raw.severityKey : '',
    policyRevision: numberValue(raw.policyRevision),
  };
}

export async function fetchPolicy(): Promise<PolicyResponse> {
  const value = await getBackendSrv().get<unknown>(`${resource}/policy`, undefined, undefined, requestOptions);
  if (!isRecord(value) || !Number.isInteger(value.revision) || !validPolicyDoc(value.doc) ||
      typeof value.defaultsApplied !== 'boolean' || !Array.isArray(value.occupancy) || !value.occupancy.every((item) => isRecord(item) &&
        typeof item.workflowId === 'string' && typeof item.stateId === 'string' && Number.isInteger(item.count))) {
    throw new Error('invalid policy response');
  }
  const response = value as unknown as PolicyResponse;
  return { ...response, doc: normalizePolicyRoles(response.doc) };
}

function validIssue(value: unknown): value is PolicyIssue {
  return isRecord(value) && typeof value.path === 'string' && typeof value.code === 'string' && typeof value.message === 'string';
}

export async function savePolicy(baseRevision: number, doc: PolicyDoc): Promise<{ revision: number }> {
  const value = await getBackendSrv().put<unknown>(`${resource}/policy`, { baseRevision, doc }, requestOptions);
  if (!isRecord(value) || !Number.isInteger(value.revision)) throw new Error('invalid policy save response');
  return value as unknown as { revision: number };
}

export async function previewPolicy(doc: PolicyDoc | undefined, samples: PolicyPreviewSample[]): Promise<PolicyPreview> {
  const value = await getBackendSrv().post<unknown>(`${resource}/policy/preview`, { ...(doc ? { doc } : {}), samples }, requestOptions);
  if (!isRecord(value) || typeof value.valid !== 'boolean' || !Array.isArray(value.issues) || !value.issues.every(validIssue) ||
      !Array.isArray(value.occupancyViolations) || !value.occupancyViolations.every((item) => isRecord(item) && typeof item.workflowId === 'string' && typeof item.stateId === 'string' && Number.isInteger(item.count)) || !Array.isArray(value.samples) ||
      !value.samples.every((sample) => isRecord(sample) && typeof sample.levelId === 'string' && typeof sample.levelMatched === 'boolean' &&
        (sample.workflowId === null || typeof sample.workflowId === 'string') && typeof sample.canStart === 'boolean' &&
        Array.isArray(sample.missingStartRequires) && sample.missingStartRequires.every((gate) => typeof gate === 'string'))) {
    throw new Error('invalid policy preview response');
  }
  return value as unknown as PolicyPreview;
}

export async function fetchPolicyRevisions(): Promise<PolicyRevision[]> {
  const value = await getBackendSrv().get<unknown>(`${resource}/policy/revisions`, undefined, undefined, requestOptions);
  if (!isRecord(value) || !Array.isArray(value.revisions) || !value.revisions.every((item) => isRecord(item) && Number.isInteger(item.revision) &&
    typeof item.createdAt === 'string' && typeof item.createdBy === 'string' && typeof item.action === 'string')) throw new Error('invalid policy revisions response');
  return value.revisions as PolicyRevision[];
}

export async function fetchPolicyRevision(revision: number): Promise<{ revision: number; doc: PolicyDoc; createdAt: string; createdBy: string }> {
  if (!Number.isSafeInteger(revision) || revision < 1) throw new Error('invalid policy revision');
  const value = await getBackendSrv().get<unknown>(`${resource}/policy/revisions/${revision}`, undefined, undefined, requestOptions);
  if (!isRecord(value) || !Number.isInteger(value.revision) || !validPolicyDoc(value.doc) || typeof value.createdAt !== 'string' || typeof value.createdBy !== 'string') throw new Error('invalid policy revision response');
  const response = value as unknown as { revision: number; doc: PolicyDoc; createdAt: string; createdBy: string };
  return { ...response, doc: normalizePolicyRoles(response.doc) };
}

function normalizeWorkflow(value: unknown): WorkflowStatus {
  if (!isRecord(value)) throw new Error('invalid workflow response');
  if (value.tracked === false && Number.isSafeInteger(value.policyRevision) && (value.policyRevision as number) > 0 && Array.isArray(value.availableWorkflows) && value.availableWorkflows.every((workflow) =>
    isRecord(workflow) && typeof workflow.id === 'string' && validName(workflow.name) && (workflow.minimumRole === 'editor' || workflow.minimumRole === 'admin')) &&
    (value.suggestedWorkflow === undefined || typeof value.suggestedWorkflow === 'string')) return value as unknown as WorkflowStatus;
  if (value.tracked === true && typeof value.workflowId === 'string' && typeof value.stateId === 'string' &&
    Number.isSafeInteger(value.revisionUsed) && (value.revisionUsed as number) > 0 && typeof value.retired === 'boolean' && typeof value.sourceConflict === 'boolean' &&
    isRecord(value.source) && typeof value.source.status === 'string' && typeof value.source.lastSuccess === 'string' &&
    typeof value.source.error === 'string' && typeof value.source.trusted === 'boolean' && typeof value.source.stale === 'boolean' &&
    Array.isArray(value.edges) && value.edges.every((edge) => isRecord(edge) && typeof edge.to === 'string' && typeof edge.allowed === 'boolean' && (edge.requiredRole === 'editor' || edge.requiredRole === 'admin') &&
      Array.isArray(edge.missing) && edge.missing.every((gate) => typeof gate === 'string'))) return value as unknown as WorkflowStatus;
  throw new Error('invalid workflow response');
}

export async function fetchWorkflow(id: string): Promise<WorkflowStatus> {
  return normalizeWorkflow(await getBackendSrv().get<unknown>(`${resource}/problems/${encodeURIComponent(id)}/workflow`, undefined, undefined, requestOptions));
}

async function workflowMutation(id: string, action: 'start' | 'transition', body: object): Promise<{ problem: Problem; workflow: WorkflowStatus }> {
  const value = await getBackendSrv().post<unknown>(`${resource}/problems/${encodeURIComponent(id)}/workflow/${action}`, body, requestOptions);
  if (!isRecord(value)) throw new Error('invalid workflow response');
  const problem = normalizeProblem(value.problem);
  if (!problem) throw new Error('invalid workflow problem');
  return { problem, workflow: normalizeWorkflow(value.workflow) };
}

export function startWorkflow(id: string, workflow: string | undefined, baseRevision: number, comment?: string) {
  if (!Number.isSafeInteger(baseRevision) || baseRevision < 1) throw new Error('invalid policy revision');
  return workflowMutation(id, 'start', { ...(workflow ? { workflow } : {}), baseRevision, ...(comment ? { comment } : {}) });
}

export function transitionWorkflow(id: string, from: string, to: string, baseRevision: number, comment?: string) {
  if (!from || !to || !Number.isSafeInteger(baseRevision) || baseRevision < 1) throw new Error('invalid workflow transition');
  return workflowMutation(id, 'transition', { from, to, baseRevision, ...(comment ? { comment } : {}) });
}

export async function fetchProblems(query: WorkspaceQuery, now: Date): Promise<Listing> {
  const response = await getBackendSrv().get<unknown>(`${resource}/problems`, buildProblemsRequest(query, now), undefined, requestOptions);
  return normalizeListing(response);
}

export async function fetchDetail(problemId: string): Promise<Detail> {
  const response = await getBackendSrv().get<Detail | undefined>(`${resource}/problems/${encodeURIComponent(problemId)}`, undefined, undefined, requestOptions);
  const problem = normalizeProblem(response?.problem);
  if (!problem || !response) {
    throw new Error('invalid detail response');
  }
  return {
    problem,
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

export async function fetchAssignees(): Promise<AssigneeCatalog> {
  return parseCatalog(await getBackendSrv().get<unknown>(`${resource}/assignees`, undefined, undefined, requestOptions));
}

function participantsFailure(error: unknown): ParticipantsError {
  if (error instanceof ParticipantsError) {
    return error;
  }
  const status = errorStatus(error);
  if (status === 403) {
    return new ParticipantsError('forbidden');
  }
  if (status === 409) {
    return new ParticipantsError('conflict');
  }
  if (status === 400 || status === 413 || status === 422) {
    return new ParticipantsError('malformed');
  }
  return new ParticipantsError('unavailable');
}

export async function updateParticipants(orgId: number, baseRevision: number): Promise<AssigneeCatalog> {
  if (!Number.isSafeInteger(orgId) || orgId <= 0 || !Number.isSafeInteger(baseRevision) || baseRevision < 0) {
    throw new ParticipantsError('org-mismatch');
  }
  let org: unknown;
  try {
    org = await getBackendSrv().get<unknown>('/api/org', undefined, undefined, requestOptions);
  } catch (error) {
    throw participantsFailure(error);
  }
  if (!isRecord(org) || !Number.isInteger(org.id)) {
    throw new ParticipantsError('malformed');
  }
  if (org.id !== orgId) {
    throw new ParticipantsError('org-mismatch');
  }
  let rows: unknown;
  try {
    rows = await getBackendSrv().get<unknown>('/api/org/users', undefined, undefined, requestOptions);
  } catch (error) {
    throw participantsFailure(error);
  }
  const members: AssigneeMember[] = participantsFromRows(rows, orgId);
  try {
    const value = await getBackendSrv().put<unknown>(`${resource}/assignees`, { orgId, baseRevision, members }, requestOptions);
    return parseCatalog(value);
  } catch (error) {
    throw participantsFailure(error);
  }
}

export async function postSync(): Promise<void> {
  await getBackendSrv().post(`${resource}/sync`, {}, requestOptions);
}
