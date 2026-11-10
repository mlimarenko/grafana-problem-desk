import { describe, expect, it } from 'vitest';
import { clearSubmittedDraft, correctedOffset, roleAllows, submitCommentDraft, validLabelFilter } from './workspace';

describe('workspace response and draft state', () => {
  it('enables only native roles meeting the required minimum', () => {
    expect(roleAllows('Editor', 'editor')).toBe(true);
    expect(roleAllows('Editor', 'admin')).toBe(false);
    expect(roleAllows('Admin', 'editor')).toBe(true);
    expect(roleAllows('Admin', 'admin')).toBe(true);
    expect(roleAllows('Viewer', 'editor')).toBe(false);
    expect(roleAllows(undefined, 'admin')).toBe(false);
  });
  it('returns the last valid offset when a refreshed page shrinks', () => {
    expect(correctedOffset(50, 50, 50)).toBe(0);
    expect(correctedOffset(100, 50, 51)).toBe(50);
    expect(correctedOffset(50, 50, 0)).toBe(0);
    expect(correctedOffset(50, 50, 51)).toBe(50);
  });

  it('clears only the submitted episode draft after success', () => {
    const submitted = 'Investigating\n';
    expect(clearSubmittedDraft({ a: submitted, b: 'other' }, 'a', submitted)).toEqual({ b: 'other' });
    const edited = { a: 'Investigating more', b: 'other' };
    expect(clearSubmittedDraft(edited, 'a', submitted)).toBe(edited);
    expect(clearSubmittedDraft({ b: 'other' }, 'a', submitted)).toEqual({ b: 'other' });
  });

  it('posts the trimmed body and preserves edits or failed drafts', async () => {
    let drafts = { a: 'Investigating\n', b: 'other' };
    const update = (reducer: (previous: Record<string, string>) => Record<string, string>) => { drafts = reducer(drafts) as typeof drafts; };
    await submitCommentDraft('a', drafts.a, async (_id, body) => { expect(body).toBe('Investigating'); }, update);
    expect(drafts).toEqual({ b: 'other' });
    drafts = { a: 'Investigating\n', b: 'other' };
    let finish!: () => void;
    const post = (_id: string, body: string) => {
      expect(body).toBe('Investigating');
      return new Promise<void>((resolve) => { finish = resolve; });
    };
    const pending = submitCommentDraft('a', drafts.a, post, update);
    drafts = { a: 'New text', b: 'other' };
    finish();
    await pending;
    expect(drafts).toEqual({ a: 'New text', b: 'other' });
    await expect(submitCommentDraft('a', drafts.a, async () => { throw new Error('offline'); }, update)).rejects.toThrow('offline');
    expect(drafts).toEqual({ a: 'New text', b: 'other' });
  });

  it('allows empty and literal values but rejects invalid label boundaries', () => {
    expect(validLabelFilter(' env ', '')).toBe(true);
    expect(validLabelFilter('env', ' 100%_ ')).toBe(true);
    expect(validLabelFilter('', 'prod')).toBe(false);
    expect(validLabelFilter('é'.repeat(129), '')).toBe(false);
    expect(validLabelFilter('env', 'é'.repeat(513))).toBe(false);
  });
});
