import React, { useCallback, useEffect, useState } from 'react';
import { css } from '@emotion/css';
import { GrafanaTheme2 } from '@grafana/data';
import { config, getBackendSrv, locationService } from '@grafana/runtime';
import { Alert, Button, Field, Input, LoadingPlaceholder, Select, TextArea, useStyles2 } from '@grafana/ui';
import { Detail, Listing, Problem, describeError, pluginId, problemTitle, safeURL, targetName } from '../model';

const resource = `/api/plugins/${pluginId}/resources`;
const pageSize = 50;
const time = (value: string) => value ? new Date(value).toLocaleString() : '—';

export function Problems() {
  const styles = useStyles2(getStyles);
  const role = config.bootData.user.orgRole;
  const canWrite = role === 'Editor' || role === 'Admin';
  const canSync = role === 'Admin';
  const [listing, setListing] = useState<Listing>();
  const [state, setState] = useState('active');
  const [severity, setSeverity] = useState('');
  const [search, setSearch] = useState('');
  const [submittedSearch, setSubmittedSearch] = useState('');
  const [offset, setOffset] = useState(0);
  const [selected, setSelected] = useState(new URLSearchParams(window.location.search).get('problem') || '');
  const [detail, setDetail] = useState<Detail>();
  const [draft, setDraft] = useState('');
  const [error, setError] = useState('');
  const [detailError, setDetailError] = useState('');
  const [busy, setBusy] = useState(false);
  const [refresh, setRefresh] = useState(0);

  useEffect(() => {
    const timer = window.setInterval(() => setRefresh(v => v + 1), 30000);
    return () => window.clearInterval(timer);
  }, []);

  useEffect(() => {
    let current = true;
    getBackendSrv().get<Listing>(`${resource}/problems`, { state, severity, search: submittedSearch, offset, limit: pageSize })
      .then(data => { if (current) { setListing(data); setError(''); } })
      .catch(error => { if (current) { setError(describeError(error)); } });
    return () => { current = false; };
  }, [state, severity, submittedSearch, offset, refresh]);

  useEffect(() => {
    if (!selected) { setDetail(undefined); return; }
    let current = true;
    getBackendSrv().get<Detail>(`${resource}/problems/${encodeURIComponent(selected)}`)
      .then(data => { if (current) { setDetail(data); setDetailError(''); } })
      .catch(error => { if (current) { setDetailError(describeError(error)); } });
    return () => { current = false; };
  }, [selected, refresh]);

  function open(problem: Problem) {
    if (problem.id !== selected && draft && !window.confirm('Discard your unsent comment?')) { return; }
    setSelected(problem.id); setDetail(undefined); setDetailError(''); setDraft('');
    locationService.partial({ problem: problem.id });
  }

  const mutate = useCallback(async (path: string, body: unknown) => {
    setBusy(true); setDetailError('');
    try {
      await getBackendSrv().post(`${resource}${path}`, body);
      setRefresh(v => v + 1);
      return true;
    } catch (error) { setDetailError(describeError(error)); return false; }
    finally { setBusy(false); }
  }, []);

  async function sync() {
    setBusy(true);
    try { await getBackendSrv().post(`${resource}/sync`, {}); setRefresh(v => v + 1); setError(''); }
    catch (error) { setError(describeError(error)); } finally { setBusy(false); }
  }

  const problem = detail?.problem;
  const generatorURL = problem && safeURL(problem.generatorURL);
  return <main className={styles.root}>
    <header className={styles.header}>
      <div><h1>Problem Desk</h1><p className={styles.muted}>Grafana alert episodes and operator history</p></div>
      <div className={styles.actions}>
        {canSync && <Button variant="secondary" icon="sync" disabled={busy} onClick={sync}>Sync source</Button>}
        {canSync && <a href={`${config.appSubUrl}/plugins/${pluginId}/configuration`}>Configuration</a>}
      </div>
    </header>

    {error && <Alert title="Could not refresh problems" severity="error">{error} Previously loaded data may be stale.</Alert>}
    {listing?.source?.error && <Alert title="Source unavailable — states may be stale" severity="warning">{listing.source.error} Problems are not marked recovered when source requests fail.</Alert>}

    <form className={styles.filters} onSubmit={e => { e.preventDefault(); setOffset(0); setSubmittedSearch(search.trim()); }}>
      <Field label="State"><Select aria-label="State" width={24} value={state} options={[{ label: 'Active', value: 'active' }, { label: 'Recovered', value: 'recovered' }, { label: 'All episodes', value: 'all' }]}
        onChange={option => { setState(option.value || 'active'); setOffset(0); }} /></Field>
      <Field label="Severity"><Select aria-label="Severity" width={24} value={severity} allowCustomValue onCreateOption={value => { setSeverity(value); setOffset(0); }} options={[{ label: 'All severities', value: '' }, { label: 'Critical', value: 'critical' }, { label: 'Warning', value: 'warning' }, { label: 'Info', value: 'info' }]}
        onChange={option => { setSeverity(option.value || ''); setOffset(0); }} /></Field>
      <Field label="Search host, rule or labels" className={styles.search}><Input aria-label="Search problems" placeholder="Host, rule, service…" value={search} onChange={e => setSearch(e.currentTarget.value)} /></Field>
      <Button variant="secondary" type="submit" icon="search">Search</Button>
    </form>

    <div className={styles.meta} role="status">
      <span>{listing ? `${listing.total} matching episodes` : 'Loading problems…'}</span>
      <span>Last successful source sync: {time(listing?.source?.lastSuccess || '')}</span>
      <span>Times use your browser&apos;s timezone</span>
    </div>

    <div className={styles.layout}>
      <section className={styles.list} aria-label="Problems">
        {!listing && !error && <LoadingPlaceholder text="Loading problems…" />}
        {listing?.problems.length === 0 && <div className={styles.empty}><h2>No matching episodes</h2><p>Change the filters or wait for the next source sync. Only observed firing alerts are collected; pending alerts and earlier history are not imported.</p></div>}
        {(listing?.problems.length || 0) > 0 && <div className={styles.tableWrap}><table className={styles.table}>
          <thead><tr><th>Severity</th><th>Problem / target</th><th>State</th><th>Started</th><th>Owner / acknowledgement</th></tr></thead>
          <tbody>{listing?.problems.map(item => <tr key={item.id} className={selected === item.id ? styles.selected : ''}>
            <td><span className={styles.severity(item.labels.severity)}>{item.labels.severity || 'Unspecified'}</span></td>
            <td><Button variant="secondary" fill="text" onClick={() => open(item)}>{problemTitle(item)}</Button><div className={styles.target}>{targetName(item)}</div></td>
            <td>{item.status === 'active' ? 'Active' : 'Recovered'}</td>
            <td>{time(item.startsAt)}</td>
            <td>{item.assignee || 'Unassigned'}<div className={styles.muted}>{item.acknowledgedBy ? `ACK · ${item.acknowledgedBy}` : 'Not acknowledged'}</div></td>
          </tr>)}</tbody>
        </table></div>}
        <div className={styles.pagination}>
          <Button variant="secondary" disabled={offset === 0} onClick={() => setOffset(v => Math.max(0, v - pageSize))}>Previous</Button>
          <span>{listing?.total ? `${offset + 1}–${Math.min(offset + pageSize, listing.total)} of ${listing.total}` : '0 episodes'}</span>
          <Button variant="secondary" disabled={!listing || offset + pageSize >= listing.total} onClick={() => setOffset(v => v + pageSize)}>Next</Button>
        </div>
      </section>

      <section className={styles.detail} aria-label="Problem details">
        {!selected && <div className={styles.empty}><h2>Select a problem</h2><p>Open an episode to read the discussion, inspect its labels or acknowledge it.</p></div>}
        {detailError && <Alert title="Problem action failed" severity="error">{detailError}</Alert>}
        {selected && !problem && !detailError && <LoadingPlaceholder text="Loading episode…" />}
        {problem && <>
          <h2>{problemTitle(problem)}</h2><p>{targetName(problem)}</p>
          <dl className={styles.facts}><dt>State</dt><dd>{problem.status}</dd><dt>Started</dt><dd>{time(problem.startsAt)}</dd><dt>Recovered</dt><dd>{time(problem.endsAt)}</dd><dt>Acknowledged by</dt><dd>{problem.acknowledgedBy || 'Nobody'}</dd><dt>Acknowledged at</dt><dd>{time(problem.acknowledgedAt)}</dd></dl>
          {generatorURL && <p><a href={generatorURL} target="_blank" rel="noopener noreferrer">Open source rule ↗</a></p>}
          {problem.annotations.description && <p className={styles.prose}>{problem.annotations.description}</p>}
          {canWrite ? <div className={styles.actions}><Button variant="secondary" disabled={busy} onClick={() => mutate(`/problems/${encodeURIComponent(problem.id)}/${problem.acknowledgedBy ? 'unack' : 'ack'}`, {})}>
            {problem.acknowledgedBy ? 'Remove acknowledgement' : 'Acknowledge'}</Button><span className={styles.muted}>Does not mute notifications or resolve the alert.</span></div> : <p className={styles.muted}>Viewer access: read-only. Editors and administrators can comment and acknowledge.</p>}

          {canWrite && <form className={styles.filters} key={`${problem.id}:${problem.assignee || ''}`} onSubmit={e => {
            e.preventDefault();
            const assignee = String(new FormData(e.currentTarget).get('assignee') || '').trim();
            void mutate(`/problems/${encodeURIComponent(problem.id)}/assign`, { assignee });
          }}>
            <Field label="Owner label" description="A Grafana login or team name; this does not grant permissions.">
              <Input aria-label="Owner label" name="assignee" defaultValue={problem.assignee || ''} maxLength={200} placeholder="Unassigned" />
            </Field>
            <Button variant="secondary" type="submit" disabled={busy}>Save owner</Button>
          </form>}

          <h3>Discussion</h3>
          {!detail.comments?.length && <p className={styles.muted}>No comments on this episode yet.</p>}
          {detail.comments?.map(comment => <article className={styles.comment} key={comment.id}><strong>{comment.author}</strong><time className={styles.muted}> · {time(comment.createdAt)}</time><p className={styles.prose}>{comment.body}</p></article>)}
          {canWrite && <form onSubmit={async e => { e.preventDefault(); if (await mutate(`/problems/${encodeURIComponent(problem.id)}/comments`, { body: draft.trim() })) { setDraft(''); } }}>
            <Field label="Comment on this episode"><TextArea aria-label="Comment on this episode" rows={4} maxLength={5000} value={draft} onChange={e => setDraft(e.currentTarget.value)} placeholder="What did you investigate or change?" /></Field>
            <Button type="submit" disabled={busy || !draft.trim()}>Add comment</Button>
          </form>}
          <h3>Activity</h3>
          {!detail.events?.length && <p className={styles.muted}>No operator actions recorded.</p>}
          {detail.events?.map(event => <p key={event.id}><strong>{event.author}</strong> · {event.kind}<br /><small className={styles.muted}>{time(event.createdAt)}</small>{event.body && <span className={styles.prose}> · {event.body}</span>}</p>)}
          <details><summary>Labels and annotations</summary><dl className={styles.labels}>{Object.entries(problem.labels).map(([key, value]) => <React.Fragment key={key}><dt>{key}</dt><dd>{value}</dd></React.Fragment>)}</dl><p className={styles.muted}>Episode {problem.id}</p></details>
        </>}
      </section>
    </div>
  </main>;
}

const getStyles = (theme: GrafanaTheme2) => ({
  root: css`padding: ${theme.spacing(3)}; max-width: 1800px; margin: auto; h1 {margin: 0;} h3 {margin-top: ${theme.spacing(3)};} @media(max-width: 600px) {padding: ${theme.spacing(1)};}`,
  header: css`display:flex; align-items:center; justify-content:space-between; gap:${theme.spacing(2)}; flex-wrap:wrap; margin-bottom:${theme.spacing(3)};`,
  actions: css`display:flex; align-items:center; gap:${theme.spacing(2)}; flex-wrap:wrap;`,
  muted: css`color:${theme.colors.text.secondary};`,
  filters: css`display:flex; align-items:flex-end; gap:${theme.spacing(2)}; flex-wrap:wrap; margin-bottom:${theme.spacing(2)}; >button {margin-bottom:${theme.spacing(2)};}`,
  search: css`flex:1; min-width:220px;`,
  meta: css`display:flex; justify-content:space-between; gap:${theme.spacing(2)}; flex-wrap:wrap; color:${theme.colors.text.secondary}; margin-bottom:${theme.spacing(2)};`,
  layout: css`display:grid; grid-template-columns:minmax(0, 3fr) minmax(340px, 2fr); gap:${theme.spacing(2)}; align-items:start; @media(max-width:1100px) {grid-template-columns:minmax(0,1fr);}`,
  list: css`min-width:0; border:1px solid ${theme.colors.border.weak}; border-radius:${theme.shape.radius.default};`,
  tableWrap: css`overflow-x:auto;`,
  table: css`width:100%; border-collapse:collapse; th,td {text-align:left; padding:${theme.spacing(1.5)}; border-bottom:1px solid ${theme.colors.border.weak}; vertical-align:top;} th {color:${theme.colors.text.secondary}; font-weight:500;} td button {height:auto; white-space:normal; text-align:left; padding:0;} th:first-child {width:110px;}`,
  target: css`margin-top:${theme.spacing(0.5)}; overflow-wrap:anywhere; color:${theme.colors.text.secondary};`,
  selected: css`background:${theme.colors.action.selected};`,
  severity: (severity: string) => css`font-weight:600; color:${severity === 'critical' ? theme.colors.error.text : severity === 'warning' ? theme.colors.warning.text : severity === 'info' ? theme.colors.info.text : theme.colors.text.primary};`,
  empty: css`padding:${theme.spacing(3)}; p {color:${theme.colors.text.secondary};}`,
  pagination: css`padding:${theme.spacing(1.5)}; display:flex; align-items:center; justify-content:space-between; gap:${theme.spacing(1)};`,
  detail: css`min-width:0; padding:${theme.spacing(2)}; border:1px solid ${theme.colors.border.weak}; border-radius:${theme.shape.radius.default}; h2 {overflow-wrap:anywhere;}`,
  facts: css`display:grid; grid-template-columns:140px minmax(0,1fr); gap:${theme.spacing(1)}; dt {color:${theme.colors.text.secondary};} dd {margin:0; overflow-wrap:anywhere;}`,
  comment: css`border-left:2px solid ${theme.colors.border.medium}; padding-left:${theme.spacing(1.5)}; margin-bottom:${theme.spacing(2)};`,
  prose: css`white-space:pre-wrap; overflow-wrap:anywhere;`,
  labels: css`display:grid; grid-template-columns:minmax(80px,1fr) minmax(0,2fr); gap:${theme.spacing(1)}; padding-top:${theme.spacing(2)}; dd {margin:0; overflow-wrap:anywhere;}`,
});
