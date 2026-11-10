export const maxAssigneeMembers = 5000;

export interface AssigneeMember {
  uid: string;
  login: string;
  name: string;
}

export interface AssigneeCatalog {
  revision: number;
  updatedAt: string;
  updatedBy: string;
  members: AssigneeMember[];
}

export interface AssigneeOption {
  label: string;
  value: string;
  isDisabled?: boolean;
}

export type ParticipantsFailure =
  | 'forbidden'
  | 'org-mismatch'
  | 'malformed'
  | 'duplicate'
  | 'over-limit'
  | 'conflict'
  | 'invalid-response'
  | 'unavailable';

export class ParticipantsError extends Error {
  constructor(readonly reason: ParticipantsFailure) {
    super(`participants ${reason}`);
    this.name = 'ParticipantsError';
  }
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return Boolean(value && typeof value === 'object' && !Array.isArray(value));
}

function boundedText(value: unknown, max: number, allowEmpty = false): value is string {
  if (typeof value !== 'string' || value.length > max || /[\u0000-\u001f\u007f]/.test(value)) {
    return false;
  }
  return allowEmpty || value.length > 0;
}

function uniqueMember(member: AssigneeMember, uids: Set<string>, logins: Set<string>): boolean {
  if (uids.has(member.uid) || logins.has(member.login)) {
    return false;
  }
  uids.add(member.uid);
  logins.add(member.login);
  return true;
}

export function parseCatalog(value: unknown): AssigneeCatalog {
  if (!isRecord(value) || !Number.isSafeInteger(value.revision) || (value.revision as number) < 0 ||
      !boundedText(value.updatedAt, 64, true) || !boundedText(value.updatedBy, 255, true) ||
      !Array.isArray(value.members) || value.members.length > maxAssigneeMembers) {
    throw new ParticipantsError('invalid-response');
  }
  const uids = new Set<string>();
  const logins = new Set<string>();
  const members = value.members.map((entry): AssigneeMember => {
    if (!isRecord(entry) || !boundedText(entry.uid, 255) || !boundedText(entry.login, 255) || !boundedText(entry.name, 255, true)) {
      throw new ParticipantsError('invalid-response');
    }
    const member: AssigneeMember = { uid: entry.uid, login: entry.login, name: entry.name };
    if (!uniqueMember(member, uids, logins)) {
      throw new ParticipantsError('invalid-response');
    }
    return member;
  });
  return {
    revision: value.revision as number,
    updatedAt: value.updatedAt as string,
    updatedBy: value.updatedBy as string,
    members,
  };
}

export function participantsFromRows(value: unknown, expectedOrgId: number): AssigneeMember[] {
  if (!Array.isArray(value)) {
    throw new ParticipantsError('malformed');
  }
  const members: AssigneeMember[] = [];
  const uids = new Set<string>();
  const logins = new Set<string>();
  for (const row of value) {
    if (!isRecord(row)) {
      throw new ParticipantsError('malformed');
    }
    if (row.orgId !== expectedOrgId) {
      throw new ParticipantsError('org-mismatch');
    }
    if (typeof row.isDisabled !== 'boolean' || !boundedText(row.uid, 255) || !boundedText(row.login, 255) || !boundedText(row.name, 255, true)) {
      throw new ParticipantsError('malformed');
    }
    if (row.isDisabled) {
      continue;
    }
    const member: AssigneeMember = { uid: row.uid, login: row.login, name: row.name };
    if (!uniqueMember(member, uids, logins)) {
      throw new ParticipantsError('duplicate');
    }
    members.push(member);
  }
  if (members.length > maxAssigneeMembers) {
    throw new ParticipantsError('over-limit');
  }
  return members;
}

export function assigneeLabel(member: AssigneeMember): string {
  const name = member.name.trim();
  return name ? `${name} (${member.login})` : member.login;
}

export function assigneeOptions(members: AssigneeMember[], current: string, legacySuffix: string): AssigneeOption[] {
  const options: AssigneeOption[] = members.map((member) => ({ label: assigneeLabel(member), value: member.login }));
  const value = current;
  if (value && !members.some((member) => member.login === value)) {
    options.unshift({ label: `${value} (${legacySuffix})`, value, isDisabled: true });
  }
  return options;
}

export function isAssigneeUnavailable(error: unknown): boolean {
  const candidate = error as { status?: number; statusCode?: number; data?: unknown } | undefined;
  const status = candidate?.status ?? candidate?.statusCode;
  if (status !== 422 || !isRecord(candidate?.data)) {
    return false;
  }
  return candidate.data.error === 'assignee_unavailable' || candidate.data.code === 'assignee_unavailable';
}
