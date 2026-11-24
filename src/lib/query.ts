import type { PolicyColumn } from '../model';

export const views = ['problems', 'recent', 'history'] as const;
export type View = (typeof views)[number];

export const rangePresets = ['1h', '24h', '7d', '30d', 'custom'] as const;
export type RangePreset = (typeof rangePresets)[number];

export type StateFilter = 'all' | 'active' | 'recovered';
export type AssignmentFilter = 'all' | 'assigned' | 'unassigned';
export type AckFilter = 'all' | 'acknowledged' | 'unacknowledged';
const sortKeys = ['severity', 'name', 'status', 'started', 'duration', 'assignee', 'ack', 'recovered'] as const;
export type SortKey = (typeof sortKeys)[number];
export type SortOrder = 'asc' | 'desc';
export type PageSize = 25 | 50 | 100;

export interface DeepLinkQuery {
  fingerprint: string;
  startsAt: string;
  matchers: string;
  ruleUID: string;
  instance: string;
  device: string;
  mountpoint: string;
  name: string;
  datname: string;
}

export interface WorkspaceQuery extends DeepLinkQuery {
  view: View;
  range: RangePreset;
  rangeExplicit: boolean;
  from: string;
  to: string;
  state: StateFilter;
  severity: string;
  workflow: string;
  workState: string;
  host: string;
  assignee: string;
  assignment: AssignmentFilter;
  ack: AckFilter;
  title: string;
  startedFrom: string;
  startedTo: string;
  minDuration: string;
  maxDuration: string;
  search: string;
  labelKey: string;
  labelValue: string;
  labelKeyPresent: boolean;
  labelValuePresent: boolean;
  sort: SortKey;
  order: SortOrder;
  limit: PageSize;
  offset: number;
  problem: string;
}

export const defaultLimit: PageSize = 50;
export const defaultSort: SortKey = 'severity';
export const defaultOrder: SortOrder = 'desc';
export const maxCustomRangeMs = 366 * 24 * 60 * 60 * 1000;

const bounds = ['title', 'startedFrom', 'startedTo', 'minDuration', 'maxDuration'] as const;
const columnScopes = {
  level: { severity: '' },
  name: { title: '', host: '' },
  status: { state: 'all' },
  started: { startedFrom: '', startedTo: '' },
  duration: { minDuration: '', maxDuration: '' },
  owner: { assignee: '', assignment: 'all' },
  ack: { ack: 'all' },
} as const satisfies Record<PolicyColumn, Partial<WorkspaceQuery>>;

export const sortByColumn: Record<PolicyColumn, SortKey> = {
  level: 'severity', name: 'name', status: 'status', started: 'started', duration: 'duration', owner: 'assignee', ack: 'ack',
};

export function clearColumnFilter(column: PolicyColumn): Partial<WorkspaceQuery> {
  return columnScopes[column];
}

export function columnFilterActive(column: PolicyColumn, query: WorkspaceQuery): boolean {
  if (column === 'status' && query.view === 'problems') return false;
  const scope: Partial<WorkspaceQuery> = columnScopes[column];
  return (Object.keys(scope) as Array<keyof WorkspaceQuery>).some((key) => scope[key] !== query[key]);
}

export function toggleSort(query: WorkspaceQuery, sort: SortKey): WorkspaceQuery {
  const defaultDirection = sort === 'severity' || sort === 'started' || sort === 'duration' || sort === 'recovered' ? 'desc' : 'asc';
  const order = query.sort === sort ? (query.order === 'asc' ? 'desc' : 'asc') : defaultDirection;
  return { ...query, sort, order, offset: 0 };
}

const presetMs: Record<Exclude<RangePreset, 'custom'>, number> = {
  '1h': 60 * 60 * 1000,
  '24h': 24 * 60 * 60 * 1000,
  '7d': 7 * 24 * 60 * 60 * 1000,
  '30d': 30 * 24 * 60 * 60 * 1000,
};

function oneOf<T extends string>(value: string | null, values: readonly T[], fallback: T): T {
  return value && (values as readonly string[]).includes(value) ? (value as T) : fallback;
}

function read(value: string | null, max: number): string {
  const text = (value ?? '').trim();
  return text.length > max ? '' : text;
}

function readRaw(value: string | null): string {
  return value ?? '';
}

function readInstant(value: string | null): string {
  const text = read(value, 64);
  if (!isRFC3339(text) || text.slice(11, 13) === '24') return '';
  const day = new Date(`${text.slice(0, 10)}T00:00:00Z`);
  const utcYear = new Date(text).getUTCFullYear();
  return day.toISOString().slice(0, 10) === text.slice(0, 10) && utcYear >= 0 && utcYear <= 9999 ? text : '';
}

export function compareInstants(left: string, right: string): number {
  const remainder = (text: string) => (/\.(\d+)/.exec(text)?.[1] ?? '').slice(3, 9).padEnd(6, '0');
  return Date.parse(left) - Date.parse(right) || Number(remainder(left)) - Number(remainder(right));
}

function readSeconds(value: string | null): string {
  const text = read(value, 16);
  const number = Number(text);
  return /^\d+$/.test(text) && Number.isSafeInteger(number) ? String(number) : '';
}

export function defaultRangeFor(view: View, defaults?: { recent: Exclude<RangePreset, 'custom'>; history: Exclude<RangePreset, 'custom'> }): RangePreset {
  return view === 'history' ? defaults?.history ?? '7d' : defaults?.recent ?? '24h';
}

export function isRFC3339(value: string): boolean {
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(value) && !Number.isNaN(Date.parse(value));
}

export type CustomRangeValidation = 'ok' | 'invalid' | 'order' | 'too-long';

export function validateCustomRange(from: string, to: string, view: View): CustomRangeValidation {
  if (!isRFC3339(from) || !isRFC3339(to)) {
    return 'invalid';
  }
  const start = Date.parse(from);
  const end = Date.parse(to);
  if (start >= end) {
    return 'order';
  }
  if (view === 'history' && end - start > maxCustomRangeMs) {
    return 'too-long';
  }
  return 'ok';
}

export function parseQuery(search: string, defaults?: { recent: Exclude<RangePreset, 'custom'>; history: Exclude<RangePreset, 'custom'> }): WorkspaceQuery {
  const params = new URLSearchParams(search.startsWith('?') ? search.slice(1) : search);
  const view = oneOf(params.get('view'), views, 'problems');
  let range = oneOf(params.get('range'), rangePresets, defaultRangeFor(view, defaults));
  let from = read(params.get('from'), 64);
  let to = read(params.get('to'), 64);
  if (range === 'custom' && validateCustomRange(from, to, view) !== 'ok') {
    range = defaultRangeFor(view, defaults);
    from = '';
    to = '';
  }
  const limitValue = Number(params.get('limit'));
  const limit: PageSize = limitValue === 25 || limitValue === 100 ? limitValue : defaultLimit;
  const offsetValue = Number(params.get('offset'));
  const offset = Number.isInteger(offsetValue) && offsetValue > 0 ? offsetValue : 0;
  const title = read(params.get('title'), 512);
  let startedFrom = readInstant(params.get('startedFrom'));
  let startedTo = readInstant(params.get('startedTo'));
  if (startedFrom && startedTo && compareInstants(startedFrom, startedTo) > 0) {
    startedFrom = startedTo = '';
  }
  let minDuration = readSeconds(params.get('minDuration'));
  let maxDuration = readSeconds(params.get('maxDuration'));
  if (minDuration && maxDuration && Number(minDuration) > Number(maxDuration)) {
    minDuration = maxDuration = '';
  }
  return {
    view,
    range,
    rangeExplicit: params.has('range'),
    from,
    to,
    state: oneOf(params.get('state'), ['all', 'active', 'recovered'], 'all'),
    severity: read(params.get('severity'), 32),
    workflow: read(params.get('workflow'), 32),
    workState: read(params.get('workState'), 32),
    host: read(params.get('host'), 256),
    assignee: read(params.get('assignee'), 256),
    assignment: oneOf(params.get('assignment'), ['all', 'assigned', 'unassigned'], 'all'),
    ack: oneOf(params.get('ack'), ['all', 'acknowledged', 'unacknowledged'], 'all'),
    title: new TextEncoder().encode(title).length <= 512 ? title : '',
    startedFrom,
    startedTo,
    minDuration,
    maxDuration,
    search: read(params.get('search'), 512),
    labelKey: readRaw(params.get('labelKey')),
    labelValue: readRaw(params.get('labelValue')),
    labelKeyPresent: params.has('labelKey'),
    labelValuePresent: params.has('labelValue'),
    sort: oneOf(params.get('sort'), sortKeys, defaultSort),
    order: oneOf(params.get('order'), ['asc', 'desc'], defaultOrder),
    limit,
    offset,
    problem: read(params.get('problem'), 200),
    fingerprint: readRaw(params.get('fingerprint')),
    startsAt: readRaw(params.get('startsAt')),
    matchers: readRaw(params.get('matchers')),
    ruleUID: readRaw(params.get('ruleUID')),
    instance: readRaw(params.get('instance')),
    device: readRaw(params.get('device')),
    mountpoint: readRaw(params.get('mountpoint')),
    name: readRaw(params.get('name')),
    datname: readRaw(params.get('datname')),
  };
}

export function hasDeepLink(query: DeepLinkQuery): boolean {
  return Boolean(
    query.fingerprint ||
      query.startsAt ||
      query.matchers ||
      query.ruleUID ||
      query.instance ||
      query.device ||
      query.mountpoint ||
      query.name ||
      query.datname
  );
}

export function clearDeepLink(query: WorkspaceQuery): WorkspaceQuery {
  return {
    ...query,
    fingerprint: '',
    startsAt: '',
    matchers: '',
    ruleUID: '',
    instance: '',
    device: '',
    mountpoint: '',
    name: '',
    datname: '',
  };
}

export function selectProblem(query: WorkspaceQuery, problemId: string): WorkspaceQuery {
  return { ...clearDeepLink(query), problem: problemId };
}

export function closeProblem(query: WorkspaceQuery): WorkspaceQuery {
  return { ...clearDeepLink(query), problem: '' };
}

export function withView(query: WorkspaceQuery, view: View, defaults?: { recent: Exclude<RangePreset, 'custom'>; history: Exclude<RangePreset, 'custom'> }): WorkspaceQuery {
  const range = query.view === 'problems' || !query.rangeExplicit ? defaultRangeFor(view, defaults) : query.range;
  return {
    ...query,
    view,
    range,
    from: range === 'custom' ? query.from : '',
    to: range === 'custom' ? query.to : '',
    state: view === 'problems' || query.view === 'problems' ? 'all' : query.state,
    offset: 0,
  };
}

export function resetFilters(query: WorkspaceQuery): WorkspaceQuery {
  return {
    ...query,
    ...columnScopes.level,
    ...columnScopes.name,
    ...columnScopes.status,
    ...columnScopes.started,
    ...columnScopes.duration,
    ...columnScopes.owner,
    ...columnScopes.ack,
    workflow: '',
    workState: '',
    search: '',
    labelKey: '',
    labelValue: '',
    labelKeyPresent: false,
    labelValuePresent: false,
    offset: 0,
  };
}

export function activeFilterCount(query: WorkspaceQuery): number {
  let count = (Object.keys(columnScopes) as PolicyColumn[]).filter((column) => columnFilterActive(column, query)).length;
  if (query.workflow) count += 1;
  if (query.workState) count += 1;
  if (query.search) count += 1;
  if (query.labelKeyPresent || query.labelValuePresent) count += 1;
  return count;
}

export interface RangeWindow {
  from: Date;
  to: Date;
}

export function rangeWindow(query: WorkspaceQuery, now: Date): RangeWindow | undefined {
  if (query.view === 'problems') {
    return undefined;
  }
  if (query.range === 'custom' && validateCustomRange(query.from, query.to, query.view) === 'ok') {
    return { from: new Date(query.from), to: new Date(query.to) };
  }
  const preset = query.range === 'custom' ? defaultRangeFor(query.view) : query.range;
  const duration = presetMs[preset as Exclude<RangePreset, 'custom'>] ?? presetMs['24h'];
  return { from: new Date(now.getTime() - duration), to: now };
}

export type ProblemsRequest = Record<string, string | number>;

export function buildProblemsRequest(query: WorkspaceQuery, now: Date): ProblemsRequest {
  const request: ProblemsRequest = {
    view: query.view,
    sort: query.sort,
    order: query.order,
    limit: query.limit,
    offset: query.offset,
  };
  const window = rangeWindow(query, now);
  if (window) {
    request.from = window.from.toISOString();
    request.to = window.to.toISOString();
    if (query.state !== 'all') {
      request.state = query.state;
    }
  }
  if (query.severity) request.severity = query.severity;
  if (query.workflow) request.workflow = query.workflow;
  if (query.workState) request.workState = query.workState;
  if (query.host) request.host = query.host;
  if (query.assignee) request.assignee = query.assignee;
  if (query.assignment !== 'all') request.assignment = query.assignment;
  if (query.ack !== 'all') request.ack = query.ack;
  for (const key of bounds) if (query[key]) request[key] = query[key];
  if (query.search) request.search = query.search;
  if (query.labelKeyPresent) request.labelKey = query.labelKey;
  if (query.labelValuePresent) request.labelValue = query.labelValue;
  return request;
}

export function serializeQuery(query: WorkspaceQuery): URLSearchParams {
  const params = new URLSearchParams();
  if (query.view !== 'problems') params.set('view', query.view);
  if (query.view !== 'problems') {
    if (query.rangeExplicit) params.set('range', query.range);
    if (query.range === 'custom') {
      if (query.from) params.set('from', query.from);
      if (query.to) params.set('to', query.to);
    }
    if (query.state !== 'all') params.set('state', query.state);
  }
  if (query.severity) params.set('severity', query.severity);
  if (query.workflow) params.set('workflow', query.workflow);
  if (query.workState) params.set('workState', query.workState);
  if (query.host) params.set('host', query.host);
  if (query.assignee) params.set('assignee', query.assignee);
  if (query.assignment !== 'all') params.set('assignment', query.assignment);
  if (query.ack !== 'all') params.set('ack', query.ack);
  for (const key of bounds) if (query[key]) params.set(key, query[key]);
  if (query.search) params.set('search', query.search);
  if (query.labelKeyPresent) params.set('labelKey', query.labelKey);
  if (query.labelValuePresent) params.set('labelValue', query.labelValue);
  if (query.sort !== defaultSort) params.set('sort', query.sort);
  if (query.order !== defaultOrder) params.set('order', query.order);
  if (query.limit !== defaultLimit) params.set('limit', String(query.limit));
  if (query.offset > 0) params.set('offset', String(query.offset));
  if (query.problem) {
    params.set('problem', query.problem);
    return params;
  }
  if (query.fingerprint) params.set('fingerprint', query.fingerprint);
  if (query.startsAt) params.set('startsAt', query.startsAt);
  if (query.matchers) params.set('matchers', query.matchers);
  if (query.ruleUID) params.set('ruleUID', query.ruleUID);
  if (query.instance) params.set('instance', query.instance);
  if (query.device) params.set('device', query.device);
  if (query.mountpoint) params.set('mountpoint', query.mountpoint);
  if (query.name) params.set('name', query.name);
  if (query.datname) params.set('datname', query.datname);
  return params;
}

export function queryString(query: WorkspaceQuery): string {
  return serializeQuery(query).toString();
}

const ownedKeys = new Set([
  'view',
  'range',
  'from',
  'to',
  'state',
  'severity',
  'workflow',
  'workState',
  'host',
  'assignee',
  'assignment',
  'ack',
  ...bounds,
  'search',
  'labelKey',
  'labelValue',
  'sort',
  'order',
  'limit',
  'offset',
  'problem',
  'fingerprint',
  'startsAt',
  'matchers',
  'ruleUID',
  'instance',
  'device',
  'mountpoint',
  'name',
  'datname',
]);

export function mergedQueryString(existingSearch: string, query: WorkspaceQuery): string {
  const existing = new URLSearchParams(existingSearch.startsWith('?') ? existingSearch.slice(1) : existingSearch);
  const merged = new URLSearchParams();
  existing.forEach((value, key) => {
    if (!ownedKeys.has(key)) {
      merged.append(key, value);
    }
  });
  serializeQuery(query).forEach((value, key) => merged.append(key, value));
  return merged.toString();
}

export function pageCount(total: number, limit: number): number {
  return Math.max(1, Math.ceil(total / Math.max(1, limit)));
}
