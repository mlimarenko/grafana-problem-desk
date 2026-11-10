import { normalizeMinimumRole, type PolicyDoc, type PolicyIssue, type Requires } from '../model';

export interface LabelRow { key: string; value: string }
export interface PolicyDraft { doc: PolicyDoc; routeLabels: LabelRow[][] }

export function labelsFromRows(rows: LabelRow[]): Record<string, string> {
  const labels: Record<string, string> = Object.create(null);
  for (const row of rows) {
    if (Object.hasOwn(labels, row.key)) throw new Error('duplicate_label');
    labels[row.key] = row.value;
  }
  return labels;
}

export function draftFromPolicy(doc: PolicyDoc): PolicyDraft {
  return {
    doc: structuredClone(doc),
    routeLabels: doc.routing.map((route) => Object.entries(route.match.labels).map(([key, value]) => ({ key, value }))),
  };
}

export interface PolicyLatest { revision: number; doc: PolicyDoc }

export interface PolicyConflict {
  /** Revision the retained draft was based on when the save was rejected. */
  baseRevision: number;
  /** Current server revision and document; absent when fetching it failed. */
  latest?: PolicyLatest;
}

export interface PolicyEditorState {
  draft?: PolicyDraft;
  /** CAS base for Save. Undefined while a conflict is unresolved. */
  revision?: number;
  conflict?: PolicyConflict;
}

export type PolicyEditorAction =
  | { type: 'loaded'; revision: number; doc: PolicyDoc }
  | { type: 'edited'; draft: PolicyDraft }
  | { type: 'saved'; revision: number }
  | { type: 'rejected'; latest?: PolicyLatest }
  | { type: 'latest'; latest: PolicyLatest }
  | { type: 'loadCurrent' }
  | { type: 'confirmReplace' };

/**
 * Keeps the draft and the rejected base revision on a CAS conflict, and only
 * adopts the current revision after an explicit load or overwrite confirmation.
 */
export function reducePolicyEditor(state: PolicyEditorState, action: PolicyEditorAction): PolicyEditorState {
  switch (action.type) {
    case 'loaded':
      return { draft: draftFromPolicy(action.doc), revision: action.revision };
    case 'edited':
      return state.draft ? { ...state, draft: action.draft } : state;
    case 'saved':
      return state.draft ? { ...state, revision: action.revision, conflict: undefined } : state;
    case 'rejected': {
      if (state.revision === undefined) return state;
      return { ...state, revision: undefined, conflict: { baseRevision: state.revision, ...(action.latest ? { latest: action.latest } : {}) } };
    }
    case 'latest':
      return state.conflict ? { ...state, conflict: { ...state.conflict, latest: action.latest } } : state;
    case 'loadCurrent': {
      const latest = state.conflict?.latest;
      return latest ? { draft: draftFromPolicy(latest.doc), revision: latest.revision } : state;
    }
    case 'confirmReplace': {
      const latest = state.conflict?.latest;
      return latest ? { ...state, revision: latest.revision, conflict: undefined } : state;
    }
  }
}

/** Save is allowed only with a concrete base revision and no unresolved conflict. */
export function policySaveRevision(state: PolicyEditorState): number | undefined {
  if (!state.draft || state.conflict) return undefined;
  return state.revision;
}

function copyRequires(value: Requires): Requires {
  const result: Requires = {};
  for (const key of ['ack', 'owner', 'comment', 'sourceRecovered'] as const) if (value[key] !== undefined) result[key] = value[key];
  return result;
}

function policyRole(value: unknown) {
  const role = normalizeMinimumRole(value);
  if (!role) throw new Error('invalid_role');
  return role;
}

// Construct only policy fields, never source settings or credentials.
export function documentFromDraft(draft: PolicyDraft): PolicyDoc {
  const doc = draft.doc;
  return {
    schemaVersion: 1,
    severityKey: doc.severityKey,
    levels: doc.levels.map((level) => ({ id: level.id, rank: level.rank, tone: level.tone, name: { ru: level.name.ru, en: level.name.en }, aliases: [...level.aliases] })),
    workflows: doc.workflows.map((workflow) => ({
      id: workflow.id, name: { ru: workflow.name.ru, en: workflow.name.en }, initialState: workflow.initialState, minimumRole: policyRole(workflow.minimumRole),
      startRequires: copyRequires(workflow.startRequires),
      states: workflow.states.map((state) => ({ id: state.id, rank: state.rank, name: { ru: state.name.ru, en: state.name.en }, ...(state.operatorComplete === undefined ? {} : { operatorComplete: state.operatorComplete }) })),
      transitions: workflow.transitions.map((transition) => ({ from: transition.from, to: transition.to, minimumRole: policyRole(transition.minimumRole), requires: copyRequires(transition.requires) })),
    })),
    routing: doc.routing.map((route, index) => ({ match: { labels: labelsFromRows(draft.routeLabels[index]), ...(route.match.level ? { level: route.match.level } : {}) }, workflow: route.workflow })),
    defaultWorkflow: doc.defaultWorkflow,
    views: { recent: doc.views.recent, history: doc.views.history, columns: [...doc.views.columns] },
    source: { recoveredTrustMinutes: doc.source.recoveredTrustMinutes },
  };
}

function object(value: unknown, keys: string[]): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value)) throw new Error('invalid_json');
  const record = value as Record<string, unknown>;
  if (Object.keys(record).some((key) => !keys.includes(key))) throw new Error('invalid_json');
  return record;
}
function text(value: unknown): void { if (typeof value !== 'string') throw new Error('invalid_json'); }
function integer(value: unknown): void { if (!Number.isInteger(value)) throw new Error('invalid_json'); }
function array(value: unknown): unknown[] { if (!Array.isArray(value)) throw new Error('invalid_json'); return value; }
function name(value: unknown): void { const item = object(value, ['en', 'ru']); text(item.en); text(item.ru); }
function requires(value: unknown): void {
  const item = object(value, ['ack', 'owner', 'comment', 'sourceRecovered']);
  if (Object.values(item).some((flag) => typeof flag !== 'boolean')) throw new Error('invalid_json');
}

export function parsePolicyJSON(json: string): PolicyDoc {
  if (new TextEncoder().encode(json).length > 48 * 1024) throw new Error('invalid_json');
  const doc = object(JSON.parse(json), ['schemaVersion', 'severityKey', 'levels', 'workflows', 'routing', 'defaultWorkflow', 'views', 'source']);
  if (doc.schemaVersion !== 1) throw new Error('invalid_json');
  text(doc.severityKey);
  for (const value of array(doc.levels)) {
    const level = object(value, ['id', 'rank', 'tone', 'name', 'aliases']);
    text(level.id); integer(level.rank); name(level.name);
    if (!['red', 'orange', 'yellow', 'blue', 'green', 'purple', 'grey'].includes(String(level.tone))) throw new Error('invalid_json');
    array(level.aliases).forEach(text);
  }
  for (const value of array(doc.workflows)) {
    const workflow = object(value, ['id', 'name', 'initialState', 'minimumRole', 'startRequires', 'states', 'transitions']);
    workflow.minimumRole = policyRole(workflow.minimumRole);
    text(workflow.id); name(workflow.name); text(workflow.initialState); requires(workflow.startRequires);
    for (const value of array(workflow.states)) {
      const state = object(value, ['id', 'rank', 'name', 'operatorComplete']);
      text(state.id); integer(state.rank); name(state.name);
      if (state.operatorComplete !== undefined && typeof state.operatorComplete !== 'boolean') throw new Error('invalid_json');
    }
    for (const value of array(workflow.transitions)) {
      const transition = object(value, ['from', 'to', 'minimumRole', 'requires']);
      transition.minimumRole = policyRole(transition.minimumRole);
      text(transition.from); text(transition.to); requires(transition.requires);
    }
  }
  for (const value of array(doc.routing)) {
    const route = object(value, ['match', 'workflow']); text(route.workflow);
    const match = object(route.match, ['labels', 'level']);
    if (match.level !== undefined) text(match.level);
    if (!match.labels || typeof match.labels !== 'object' || Array.isArray(match.labels)) throw new Error('invalid_json');
    Object.values(match.labels).forEach(text);
  }
  if (doc.defaultWorkflow !== null) text(doc.defaultWorkflow);
  const views = object(doc.views, ['recent', 'history', 'columns']);
  if (!['1h', '24h', '7d', '30d'].includes(String(views.recent)) || !['1h', '24h', '7d', '30d'].includes(String(views.history))) throw new Error('invalid_json');
  if (array(views.columns).some((column) => !['level', 'name', 'status', 'started', 'duration', 'owner', 'ack'].includes(String(column)))) throw new Error('invalid_json');
  const source = object(doc.source, ['recoveredTrustMinutes']); integer(source.recoveredTrustMinutes);
  return doc as unknown as PolicyDoc;
}

export function localizePolicyIssue(issue: PolicyIssue, labels: Record<string, string>, fallback: string): string {
  const translated = Object.hasOwn(labels, issue.code) ? labels[issue.code] : fallback;
  return `${issue.path}: ${translated} (${issue.code})${issue.message ? ` — ${issue.message}` : ''}`;
}
