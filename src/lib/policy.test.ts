import { describe, expect, it } from 'vitest';
import { documentFromDraft, draftFromPolicy, labelsFromRows, localizePolicyIssue, parsePolicyJSON, policySaveRevision, reducePolicyEditor } from './policy';
import type { PolicyDoc } from '../model';

const doc: PolicyDoc = {
  schemaVersion: 1,
  severityKey: 'priority',
  levels: [{ id: 'urgent', rank: 8, tone: 'red', name: { en: 'Urgent', ru: 'Срочно' }, aliases: [' urgent ', 'critical'] }],
  workflows: [{ id: 'ops', name: { en: 'Operations', ru: 'Операции' }, initialState: 'new', minimumRole: 'admin', startRequires: { ack: true }, states: [{ id: 'new', rank: 1, name: { en: 'New', ru: 'Новый' } }], transitions: [{ from: 'new', to: 'done', minimumRole: 'admin', requires: { comment: true } }] }],
  routing: [{ match: { labels: { team: ' payments ' }, level: 'urgent' }, workflow: 'ops' }],
  defaultWorkflow: null,
  views: { recent: '1h', history: '30d', columns: ['level', 'name'] },
  source: { recoveredTrustMinutes: 90 },
};

describe('policy editor boundaries', () => {
  it('round-trips a policy and preserves literal labels and alias whitespace', () => {
    const draft = draftFromPolicy(doc);
    draft.routeLabels[0].push({ key: 'empty', value: '' });
    const output = documentFromDraft(draft);
    const imported = parsePolicyJSON(JSON.stringify(output));
    expect(imported).toEqual(output);
    expect(imported.routing[0].match.labels).toEqual({ team: ' payments ', empty: '' });
    expect(imported.levels[0].aliases[0]).toBe(' urgent ');
    expect(imported.workflows[0].minimumRole).toBe('admin');
    expect(imported.workflows[0].transitions[0].minimumRole).toBe('admin');
  });

  it('defaults omitted or empty roles to editor and rejects unknown roles', () => {
    const input = JSON.parse(JSON.stringify(doc));
    delete input.workflows[0].minimumRole;
    input.workflows[0].transitions[0].minimumRole = '';
    const parsed = parsePolicyJSON(JSON.stringify(input));
    expect(parsed.workflows[0].minimumRole).toBe('editor');
    expect(parsed.workflows[0].transitions[0].minimumRole).toBe('editor');
    input.workflows[0].minimumRole = 'viewer';
    expect(() => parsePolicyJSON(JSON.stringify(input))).toThrow('invalid_role');
    input.workflows[0].minimumRole = 'editor';
    input.workflows[0].transitions[0].minimumRole = 'owner';
    expect(() => parsePolicyJSON(JSON.stringify(input))).toThrow('invalid_role');
  });

  it('keeps prototype-shaped label keys and rejects duplicate keys', () => {
    const record = labelsFromRows([{ key: '__proto__', value: 'a' }, { key: 'constructor', value: 'b' }]);
    expect(Object.getPrototypeOf(record)).toBeNull();
    expect(record.__proto__).toBe('a');
    expect(record.constructor).toBe('b');
    expect(() => labelsFromRows([{ key: 'a', value: '' }, { key: 'a', value: 'b' }])).toThrow('duplicate_label');
  });

  it('rejects unsupported fields on import and projects only policy fields on export', () => {
    const foreign = { ...doc, secureJsonData: { grafanaToken: 'must-not-export' } };
    expect(() => parsePolicyJSON(JSON.stringify(foreign))).toThrow();
    expect(() => parsePolicyJSON(JSON.stringify({ ...doc, source: { ...doc.source, credentials: 'secret' } }))).toThrow();
    expect(JSON.stringify(documentFromDraft(draftFromPolicy(foreign)))).not.toContain('must-not-export');
    expect(() => parsePolicyJSON('{')).toThrow();
  });

  it('renders issue paths and server detail with a localized known code or a safe fallback', () => {
    expect(localizePolicyIssue({ path: '$.levels[0].id', code: 'duplicate', message: 'already used' }, { duplicate: 'Повтор' }, 'Ошибка')).toBe('$.levels[0].id: Повтор (duplicate) — already used');
    expect(localizePolicyIssue({ path: '$', code: '__proto__', message: '' }, {}, 'Ошибка')).toBe('$: Ошибка (__proto__)');
  });
});

describe('policy editor conflict resolution', () => {
  function rejectedDraft() {
    const state = reducePolicyEditor({}, { type: 'loaded', revision: 1, doc });
    const draft = draftFromPolicy(doc);
    draft.doc.levels[0].rank = 5;
    draft.doc.levels[0].name.en = 'Highest urgency';
    return reducePolicyEditor(state, { type: 'edited', draft });
  }
  const peerDoc = structuredClone(doc);
  peerDoc.levels[0].rank = 94;

  it('keeps the draft and blocks saving after a stale save is rejected', () => {
    const edited = rejectedDraft();
    expect(policySaveRevision(edited)).toBe(1);
    const conflicted = reducePolicyEditor(edited, { type: 'rejected', latest: { revision: 2, doc: peerDoc } });
    expect(policySaveRevision(conflicted)).toBeUndefined();
    expect(conflicted.draft).toEqual(edited.draft);
    expect(conflicted.draft?.doc.levels[0].rank).toBe(5);
    expect(conflicted.draft?.doc.levels[0].name.en).toBe('Highest urgency');
    expect(conflicted.conflict).toEqual({ baseRevision: 1, latest: { revision: 2, doc: peerDoc } });
  });

  it('stays blocked when the current revision cannot be fetched and allows an explicit retry', () => {
    const conflicted = reducePolicyEditor(rejectedDraft(), { type: 'rejected' });
    expect(policySaveRevision(conflicted)).toBeUndefined();
    expect(conflicted.conflict?.latest).toBeUndefined();
    expect(reducePolicyEditor(conflicted, { type: 'confirmReplace' })).toBe(conflicted);
    expect(reducePolicyEditor(conflicted, { type: 'loadCurrent' })).toBe(conflicted);
    const retried = reducePolicyEditor(conflicted, { type: 'latest', latest: { revision: 2, doc: peerDoc } });
    expect(policySaveRevision(retried)).toBeUndefined();
    expect(retried.conflict?.latest?.revision).toBe(2);
    expect(retried.draft?.doc.levels[0].rank).toBe(5);
  });

  it('adopts the current document only when the user explicitly loads it', () => {
    const conflicted = reducePolicyEditor(rejectedDraft(), { type: 'rejected', latest: { revision: 2, doc: peerDoc } });
    const loaded = reducePolicyEditor(conflicted, { type: 'loadCurrent' });
    expect(policySaveRevision(loaded)).toBe(2);
    expect(loaded.conflict).toBeUndefined();
    expect(loaded.draft?.doc.levels[0].rank).toBe(94);
  });

  it('saves the retained draft on the latest revision after explicit confirmation and re-blocks on a newer peer change', () => {
    const conflicted = reducePolicyEditor(rejectedDraft(), { type: 'rejected', latest: { revision: 2, doc: peerDoc } });
    const replacement = reducePolicyEditor(conflicted, { type: 'confirmReplace' });
    expect(policySaveRevision(replacement)).toBe(2);
    expect(replacement.conflict).toBeUndefined();
    expect(replacement.draft?.doc.levels[0].rank).toBe(5);

    const peerDoc3 = structuredClone(peerDoc);
    peerDoc3.levels[0].rank = 97;
    const rejectedAgain = reducePolicyEditor(replacement, { type: 'rejected', latest: { revision: 3, doc: peerDoc3 } });
    expect(policySaveRevision(rejectedAgain)).toBeUndefined();
    expect(rejectedAgain.conflict?.baseRevision).toBe(2);
    expect(rejectedAgain.draft?.doc.levels[0].rank).toBe(5);
    const confirmedAgain = reducePolicyEditor(rejectedAgain, { type: 'confirmReplace' });
    expect(policySaveRevision(confirmedAgain)).toBe(3);

    const saved = reducePolicyEditor(confirmedAgain, { type: 'saved', revision: 4 });
    expect(policySaveRevision(saved)).toBe(4);
    expect(saved.conflict).toBeUndefined();
  });
});
