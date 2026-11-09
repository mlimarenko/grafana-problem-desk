import { isRFC3339, type WorkspaceQuery } from './query';

export type DeepLinkError = 'fingerprint' | 'matchers' | 'fields';

export interface ResolveRequest {
  mode: 'fingerprint' | 'matchers';
  params: Record<string, string>;
}

export type DeepLinkResolution = { request: ResolveRequest } | { error: DeepLinkError } | undefined;

const matcherKeys = ['__alert_rule_uid__', 'instance', 'device', 'mountpoint', 'name', 'datname'] as const;
const matcherKeySet = new Set<string>(matcherKeys);
const optionalFieldKeys = ['device', 'mountpoint', 'name', 'datname'] as const;
const encoder = new TextEncoder();

function byteLength(value: string): number {
  return encoder.encode(value).length;
}

function validValue(value: unknown, max: number): value is string {
  return typeof value === 'string' && value.length > 0 && byteLength(value) <= max;
}

export function matchersFromFields(query: WorkspaceQuery): { matchers?: Record<string, string>; valid: boolean } {
  const ruleUID = query.ruleUID;
  const instance = query.instance;
  if (!validValue(ruleUID, 256) || !validValue(instance, 1024)) {
    return { valid: false };
  }
  const matchers: Record<string, string> = { __alert_rule_uid__: ruleUID, instance };
  for (const key of optionalFieldKeys) {
    const value = query[key];
    if (value) {
      if (byteLength(value) > 1024) {
        return { valid: false };
      }
      matchers[key] = value;
    }
  }
  return { matchers, valid: true };
}

export function parseMatchers(raw: string): { matchers?: Record<string, string>; valid: boolean } {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return { valid: false };
  }
  if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
    return { valid: false };
  }
  const matchers: Record<string, string> = {};
  for (const [key, value] of Object.entries(parsed as Record<string, unknown>)) {
    if (!matcherKeySet.has(key) || typeof value !== 'string' || byteLength(value) > 4096) {
      return { valid: false };
    }
    if (value) {
      matchers[key] = value;
    }
  }
  if (!validValue(matchers.__alert_rule_uid__, 256) || !validValue(matchers.instance, 1024)) {
    return { valid: false };
  }
  return { matchers, valid: true };
}

export function resolveDeepLink(query: WorkspaceQuery): DeepLinkResolution {
  if (query.problem) {
    return undefined;
  }
  if (byteLength(query.fingerprint) > 1024 || byteLength(query.startsAt) > 64) {
    return { error: 'fingerprint' };
  }
  if (byteLength(query.matchers) > 16384) {
    return { error: 'matchers' };
  }
  if (byteLength(query.ruleUID) > 256 || byteLength(query.instance) > 1024 || optionalFieldKeys.some((key) => byteLength(query[key]) > 1024)) {
    return { error: 'fields' };
  }
  if (query.fingerprint || query.startsAt) {
    if (!query.fingerprint || !query.startsAt || query.fingerprint.length > 1024 || !isRFC3339(query.startsAt)) {
      return { error: 'fingerprint' };
    }
    return {
      request: {
        mode: 'fingerprint',
        params: { fingerprint: query.fingerprint, startsAt: query.startsAt },
      },
    };
  }
  if (query.matchers) {
    const parsed = parseMatchers(query.matchers);
    if (!parsed.valid || !parsed.matchers) {
      return { error: 'matchers' };
    }
    return { request: { mode: 'matchers', params: { matchers: JSON.stringify(parsed.matchers) } } };
  }
  if (query.ruleUID || query.instance || optionalFieldKeys.some((key) => query[key])) {
    const built = matchersFromFields(query);
    if (!built.valid || !built.matchers) {
      return { error: 'fields' };
    }
    return { request: { mode: 'matchers', params: { matchers: JSON.stringify(built.matchers) } } };
  }
  return undefined;
}
