import { describe, expect, it } from 'vitest';
import {
  ParticipantsError,
  assigneeLabel,
  assigneeOptions,
  isAssigneeUnavailable,
  maxAssigneeMembers,
  parseCatalog,
  participantsFromRows,
  type AssigneeMember,
} from './assignees';

const row = (overrides: Record<string, unknown> = {}) => ({
  orgId: 1,
  userId: 2,
  uid: 'uid-editor',
  email: 'editor@example.test',
  name: 'Synthetic Editor',
  avatarUrl: 'https://example.test/avatar.png',
  login: 'dev227-editor',
  role: 'Editor',
  isDisabled: false,
  isServiceAccount: null,
  ...overrides,
});

describe('participant projection', () => {
  it('keeps only uid, login and name of enabled users', () => {
    const members = participantsFromRows([row(), row({ uid: 'uid-viewer', login: 'viewer', name: '', role: 'Viewer' })], 1);
    expect(members).toEqual([
      { uid: 'uid-editor', login: 'dev227-editor', name: 'Synthetic Editor' },
      { uid: 'uid-viewer', login: 'viewer', name: '' },
    ]);
    expect(Object.keys(members[0]).sort()).toEqual(['login', 'name', 'uid']);
  });

  it('excludes disabled users and keeps a complete empty roster valid', () => {
    expect(participantsFromRows([row({ isDisabled: true })], 1)).toEqual([]);
    expect(participantsFromRows([], 1)).toEqual([]);
  });

  it('rejects an incomplete or malformed organization response', () => {
    for (const value of [undefined, null, {}, 'users', [row({ orgId: '1' })], [row({ isDisabled: undefined })], [row({ uid: '' })], [row({ login: 'bad\nlogin' })]]) {
      expect(() => participantsFromRows(value, 1)).toThrow(ParticipantsError);
    }
  });

  it('rejects rows from another organization instead of publishing a partial roster', () => {
    expect(() => participantsFromRows([row(), row({ orgId: 2, login: 'other' })], 1)).toThrowError(
      expect.objectContaining({ reason: 'org-mismatch' })
    );
  });

  it('rejects duplicate uid and login values', () => {
    expect(() => participantsFromRows([row(), row({ userId: 3 })], 1)).toThrowError(expect.objectContaining({ reason: 'duplicate' }));
    expect(() => participantsFromRows([row(), row({ uid: 'other-uid' })], 1)).toThrowError(expect.objectContaining({ reason: 'duplicate' }));
  });

  it('rejects a roster above the member limit', () => {
    const rows = Array.from({ length: maxAssigneeMembers + 1 }, (_, index) =>
      row({ uid: `uid-${index}`, login: `user-${index}`, name: '' })
    );
    expect(() => participantsFromRows(rows, 1)).toThrowError(expect.objectContaining({ reason: 'over-limit' }));
  });
});

describe('assignee catalog', () => {
  it('keeps a removed literal login distinct from a trimmed participant', () => {
    expect(assigneeOptions([{ uid: 'plain', login: 'boundary', name: 'Plain' }], ' boundary ', 'legacy')).toEqual([
      { label: ' boundary  (legacy)', value: ' boundary ', isDisabled: true },
      { label: 'Plain (boundary)', value: 'boundary' },
    ]);
    expect(assigneeOptions([{ uid: 'spaced', login: ' boundary ', name: 'Spaced' }], ' boundary ', 'legacy')).toEqual([
      { label: 'Spaced ( boundary )', value: ' boundary ' },
    ]);
  });

  it('accepts an unpublished catalog with revision zero and no members', () => {
    expect(parseCatalog({ revision: 0, updatedAt: '', updatedBy: '', members: [] })).toEqual({
      revision: 0,
      updatedAt: '',
      updatedBy: '',
      members: [],
    });
  });

  it('rejects malformed members and duplicate logins from the resource', () => {
    const member = { uid: 'u1', login: 'alice', name: 'Alice' };
    for (const value of [
      undefined,
      { revision: -1, updatedAt: '', updatedBy: '', members: [] },
      { revision: 1, updatedAt: '', updatedBy: 'admin', members: [{ uid: 'u1', login: '', name: '' }] },
      { revision: 1, updatedAt: '', updatedBy: 'admin', members: [member, member] },
      { revision: 1, updatedAt: '', updatedBy: 'admin', members: [{ uid: 'u1', login: 'a\u0000', name: '' }] },
    ]) {
      expect(() => parseCatalog(value)).toThrowError(expect.objectContaining({ reason: 'invalid-response' }));
    }
  });
});

describe('assignee selection', () => {
  const members: AssigneeMember[] = [
    { uid: 'u1', login: 'dev227-editor', name: 'Synthetic Editor' },
    { uid: 'u2', login: 'viewer', name: ' ' },
  ];

  it('labels options by display name and login, falling back to login', () => {
    expect(assigneeLabel(members[0])).toBe('Synthetic Editor (dev227-editor)');
    expect(assigneeLabel(members[1])).toBe('viewer');
    expect(assigneeOptions(members, '', 'not in participant list')).toEqual([
      { label: 'Synthetic Editor (dev227-editor)', value: 'dev227-editor' },
      { label: 'viewer', value: 'viewer' },
    ]);
  });

  it('keeps a legacy assignment visible as a single disabled current option', () => {
    expect(assigneeOptions(members, 'former-team', 'not in participant list')).toEqual([
      { label: 'former-team (not in participant list)', value: 'former-team', isDisabled: true },
      { label: 'Synthetic Editor (dev227-editor)', value: 'dev227-editor' },
      { label: 'viewer', value: 'viewer' },
    ]);
  });

  it('offers no option for an empty or already listed value', () => {
    expect(assigneeOptions(members, '', 'legacy').map((option) => option.value)).toEqual(['dev227-editor', 'viewer']);
    expect(assigneeOptions(members, 'dev227-editor', 'legacy').map((option) => option.value)).toEqual(['dev227-editor', 'viewer']);
  });

  it('recognizes only the documented unavailable-assignee response', () => {
    expect(isAssigneeUnavailable({ status: 422, data: { error: 'assignee_unavailable' } })).toBe(true);
    expect(isAssigneeUnavailable({ status: 422, data: { code: 'assignee_unavailable' } })).toBe(true);
    expect(isAssigneeUnavailable({ status: 422, data: { error: 'other' } })).toBe(false);
    expect(isAssigneeUnavailable({ status: 422, data: undefined })).toBe(false);
    expect(isAssigneeUnavailable({ status: 400, data: { error: 'assignee_unavailable' } })).toBe(false);
    expect(isAssigneeUnavailable(new Error('socket closed'))).toBe(false);
  });
});
