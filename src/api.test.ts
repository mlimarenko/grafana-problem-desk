import { describe, expect, it, vi } from 'vitest';

vi.mock('@grafana/runtime', () => ({ getBackendSrv: vi.fn() }));

import { getBackendSrv } from '@grafana/runtime';
import { fetchPolicy, fetchWorkflow, normalizeListing, previewPolicy, savePolicy, startWorkflow, transitionWorkflow } from './api';

describe('listing normalization', () => {
  it('preserves level counts in a prototype-safe dictionary', () => {
    const input = JSON.parse('{"summary":{"byLevel":{"critical":4,"__proto__":2,"constructor":3,"invalid":"x"}}}');
    const listing = normalizeListing(input);
    expect(Object.getPrototypeOf(listing.summary.byLevel)).toBeNull();
    expect(listing.summary.byLevel.critical).toBe(4);
    expect(listing.summary.byLevel.__proto__).toBe(2);
    expect(listing.summary.byLevel.constructor).toBe(3);
    expect(listing.summary.byLevel.invalid).toBe(0);
  });
});

describe('policy and workflow response boundaries', () => {
  it('accepts a well-formed policy and rejects a malformed workflow collection', async () => {
    const response = {
      revision: 1, defaultsApplied: false, occupancy: [],
      doc: { schemaVersion: 1, severityKey: 'priority', levels: [{ id: 'low', rank: 1, tone: 'blue', name: { ru: 'Низкий', en: 'Low' }, aliases: [] }],
        workflows: [], routing: [], defaultWorkflow: null, views: { recent: '1h', history: '7d', columns: ['name'] }, source: { recoveredTrustMinutes: 60 } },
    };
    const get = vi.fn().mockResolvedValue(response);
    vi.mocked(getBackendSrv).mockReturnValue({ get } as unknown as ReturnType<typeof getBackendSrv>);
    expect((await fetchPolicy()).doc.severityKey).toBe('priority');
    get.mockResolvedValue({ ...response, doc: { ...response.doc, workflows: 'invalid' } });
    await expect(fetchPolicy()).rejects.toThrow('invalid policy response');
  });

  it('rejects malformed workflow edges rather than enabling them', async () => {
    const get = vi.fn().mockResolvedValue({ tracked: true, workflowId: 'basic', stateId: 'new', revisionUsed: 1, retired: false, sourceConflict: false,
      source: { status: 'active', lastSuccess: '', error: '', trusted: false, stale: true }, edges: [{ to: 'done', allowed: 'yes', requiredRole: 'editor', missing: [] }] });
    vi.mocked(getBackendSrv).mockReturnValue({ get } as unknown as ReturnType<typeof getBackendSrv>);
    await expect(fetchWorkflow('p')).rejects.toThrow('invalid workflow response');
  });

  it('requires status revision and role metadata at the workflow boundary', async () => {
    const get = vi.fn().mockResolvedValue({ tracked: false, policyRevision: 4, availableWorkflows: [{ id: 'ops', name: { ru: 'Работа', en: 'Ops' }, minimumRole: 'admin' }] });
    vi.mocked(getBackendSrv).mockReturnValue({ get } as unknown as ReturnType<typeof getBackendSrv>);
    expect(await fetchWorkflow('p')).toMatchObject({ policyRevision: 4, availableWorkflows: [{ minimumRole: 'admin' }] });
    get.mockResolvedValue({ tracked: false, availableWorkflows: [] });
    await expect(fetchWorkflow('p')).rejects.toThrow('invalid workflow response');
    get.mockResolvedValue({ tracked: false, policyRevision: 4, availableWorkflows: [{ id: 'ops', name: { ru: 'Работа', en: 'Ops' }, minimumRole: 'viewer' }] });
    await expect(fetchWorkflow('p')).rejects.toThrow('invalid workflow response');
    const tracked = { tracked: true, workflowId: 'ops', stateId: 'new', revisionUsed: 4, retired: false, sourceConflict: false, source: { status: 'active', lastSuccess: '', error: '', trusted: false, stale: true }, edges: [{ to: 'done', allowed: false, requiredRole: 'admin', missing: ['role'] }] };
    get.mockResolvedValue(tracked);
    expect(await fetchWorkflow('p')).toMatchObject({ edges: [{ requiredRole: 'admin', missing: ['role'] }] });
    get.mockResolvedValue({ ...tracked, edges: [{ to: 'done', allowed: true, missing: [] }] });
    await expect(fetchWorkflow('p')).rejects.toThrow('invalid workflow response');
  });

  it('normalizes omitted policy roles and rejects unknown policy roles', async () => {
    const workflow = { id: 'ops', name: { en: 'Ops', ru: 'Операции' }, initialState: 'new', startRequires: {}, states: [], transitions: [{ from: 'new', to: 'done', minimumRole: 'admin', requires: {} }] };
    const response = { revision: 1, defaultsApplied: false, occupancy: [], doc: { schemaVersion: 1, severityKey: 'priority', levels: [], workflows: [workflow], routing: [], defaultWorkflow: null, views: { recent: '1h', history: '7d', columns: ['name'] }, source: { recoveredTrustMinutes: 60 } } };
    const get = vi.fn().mockResolvedValue(response);
    vi.mocked(getBackendSrv).mockReturnValue({ get } as unknown as ReturnType<typeof getBackendSrv>);
    const policy = await fetchPolicy();
    expect(policy.doc.workflows[0].minimumRole).toBe('editor');
    expect(policy.doc.workflows[0].transitions[0].minimumRole).toBe('admin');
    get.mockResolvedValue({ ...response, doc: { ...response.doc, workflows: [{ ...workflow, minimumRole: 'viewer' }] } });
    await expect(fetchPolicy()).rejects.toThrow('invalid policy response');
  });

  it('sends the rendered revision and source state on workflow mutations', async () => {
    const post = vi.fn().mockResolvedValue({ problem: { id: 'p', status: 'active' }, workflow: { tracked: true, workflowId: 'ops', stateId: 'new', revisionUsed: 7, retired: false, sourceConflict: false, source: { status: 'active', lastSuccess: '', error: '', trusted: false, stale: true }, edges: [] } });
    vi.mocked(getBackendSrv).mockReturnValue({ post } as unknown as ReturnType<typeof getBackendSrv>);
    await startWorkflow('p', 'ops', 7, 'starting');
    expect(post.mock.calls[0][1]).toEqual({ workflow: 'ops', baseRevision: 7, comment: 'starting' });
    await transitionWorkflow('p', 'new', 'in_progress', 7, 'working');
    expect(post.mock.calls[1][1]).toEqual({ from: 'new', to: 'in_progress', baseRevision: 7, comment: 'working' });
    await startWorkflow('p', undefined, 8);
    expect(post.mock.calls[2][1]).toEqual({ baseRevision: 8 });
    expect(() => startWorkflow('p', 'ops', 0)).toThrow('invalid policy revision');
    expect(() => transitionWorkflow('p', '', 'done', 7)).toThrow('invalid workflow transition');
    expect(post).toHaveBeenCalledTimes(3);
  });

  it('sends samples literally and reads occupancy violations and CAS revisions', async () => {
    const post = vi.fn().mockResolvedValue({ valid: false, issues: [], occupancyViolations: [{ workflowId: 'ops', stateId: 'new', count: 2 }], samples: [{ levelId: 'urgent', levelMatched: true, workflowId: 'ops', canStart: false, missingStartRequires: ['ack'] }] });
    const put = vi.fn().mockResolvedValue({ revision: 8 });
    vi.mocked(getBackendSrv).mockReturnValue({ post, put } as unknown as ReturnType<typeof getBackendSrv>);
    const sample = { labels: { team: ' payments ' } };
    const result = await previewPolicy(undefined, [sample]);
    expect(result.occupancyViolations[0]).toEqual({ workflowId: 'ops', stateId: 'new', count: 2 });
    expect(post.mock.calls[0][1]).toEqual({ samples: [sample] });
    const doc = { schemaVersion: 1 } as const;
    expect(await savePolicy(7, doc as Parameters<typeof savePolicy>[1])).toEqual({ revision: 8 });
    expect(put.mock.calls[0][1]).toEqual({ baseRevision: 7, doc });
  });
});
