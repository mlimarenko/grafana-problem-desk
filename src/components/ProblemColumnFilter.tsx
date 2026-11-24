import React, { useState } from 'react';
import { css } from '@emotion/css';
import type { GrafanaTheme2 } from '@grafana/data';
import { Button, Field, IconButton, Input, RadioButtonList, Toggletip, useStyles2 } from '@grafana/ui';
import { useI18n } from '../i18n';
import { dateTimeLocalToISO, toDateTimeLocalValue } from '../i18n/format';
import { clearColumnFilter, columnFilterActive, compareInstants, type SortKey, type WorkspaceQuery } from '../lib/query';
import { compareLevels, type Level, type Listing, type PolicyColumn } from '../model';

export interface ColumnFilterProps {
  query: WorkspaceQuery;
  levels: Level[];
  facets?: Listing['facets'];
  onFilter: (patch: Partial<WorkspaceQuery>) => void;
  onSort: (key: SortKey) => void;
}

export function columnLabels({ table }: ReturnType<typeof useI18n>['messages']): Record<PolicyColumn, string> {
  return {
    level: table.severity, name: table.problem, status: table.status, started: table.started,
    duration: table.duration, owner: table.owner, ack: table.ack,
  };
}

export function ProblemColumnFilter(props: ColumnFilterProps & { column: PolicyColumn; labeled?: boolean }) {
  const { column, query, labeled } = props;
  const { messages: m } = useI18n();
  const [open, setOpen] = useState(false);
  if (column === 'status' && query.view === 'problems') return null;
  const label = columnLabels(m)[column];
  const active = columnFilterActive(column, query);
  const title = m.filters.column(label);
  const accessibleLabel = m.filters.column(label, active);

  return (
    <Toggletip
      placement="bottom-start"
      title={title}
      show={open}
      onOpen={() => setOpen(true)}
      onClose={() => setOpen(false)}
      content={<FilterEditor {...props} close={() => setOpen(false)} />}
    >
      {labeled ? (
        <Button type="button" variant={active ? 'primary' : 'secondary'} fill="outline" icon="filter" aria-label={accessibleLabel}>
          {label}
        </Button>
      ) : (
        <IconButton
          type="button"
          name="filter"
          size="sm"
          variant={active ? 'primary' : 'secondary'}
          aria-label={accessibleLabel}
          title={title}
        />
      )}
    </Toggletip>
  );
}

function asSeconds(minutes: string): string | undefined {
  if (!minutes) return '';
  const value = Math.round(Number(minutes) * 60);
  return Number(minutes) >= 0 && Number.isSafeInteger(value) ? String(value) : undefined;
}

function FilterEditor({ column, query, levels, facets, onFilter, onSort, close }: ColumnFilterProps & {
  column: PolicyColumn;
  close: () => void;
}) {
  const styles = useStyles2(getStyles);
  const { messages: m, language, timeZone, zoneLabel } = useI18n();
  const [draft, setDraft] = useState(query);
  const inputDate = (value: string) => toDateTimeLocalValue(value, timeZone, true);
  const [from, setFrom] = useState(() => inputDate(query.startedFrom));
  const [to, setTo] = useState(() => inputDate(query.startedTo));
  const [min, setMin] = useState(() => query.minDuration ? String(Number(query.minDuration) / 60) : '');
  const [max, setMax] = useState(() => query.maxDuration ? String(Number(query.maxDuration) / 60) : '');
  const [error, setError] = useState('');
  const change = (patch: Partial<WorkspaceQuery>) => {
    setDraft((value) => ({ ...value, ...patch }));
    setError('');
  };

  function apply(event: React.FormEvent) {
    event.preventDefault();
    let patch: Partial<WorkspaceQuery>;
    switch (column) {
      case 'level': patch = { severity: draft.severity }; break;
      case 'name': patch = { title: draft.title.trim(), host: draft.host.trim() }; break;
      case 'status': patch = { state: draft.state }; break;
      case 'owner': patch = { assignee: draft.assignment === 'unassigned' ? '' : draft.assignee.trim(), assignment: draft.assignment }; break;
      case 'ack': patch = { ack: draft.ack }; break;
      case 'started': {
        const parse = (input: string, prior: string) => input === '' ? '' : input === inputDate(prior) ? prior : dateTimeLocalToISO(input, timeZone);
        const startedFrom = parse(from, query.startedFrom);
        const startedTo = parse(to, query.startedTo);
        if (startedFrom === undefined || startedTo === undefined) return setError(m.range.invalidDate);
        if (startedFrom && startedTo && compareInstants(startedFrom, startedTo) > 0) return setError(m.range.invalidOrder);
        patch = { startedFrom, startedTo };
        break;
      }
      case 'duration': {
        const minDuration = asSeconds(min);
        const maxDuration = asSeconds(max);
        if (minDuration === undefined || maxDuration === undefined || (minDuration && maxDuration && Number(minDuration) > Number(maxDuration))) {
          return setError(m.filters.invalidDuration);
        }
        patch = { minDuration, maxDuration };
        break;
      }
    }
    const encoder = new TextEncoder();
    if (encoder.encode(patch.title ?? '').length > 512 || encoder.encode(patch.host ?? '').length > 256 ||
        encoder.encode(patch.assignee ?? '').length > 256) {
      return setError(m.filters.textTooLong);
    }
    onFilter(patch);
    close();
  }

  const statusOptions = [
    { value: 'all', label: m.filters.stateAll },
    { value: 'active', label: m.filters.stateActive },
    { value: 'recovered', label: m.filters.stateRecovered },
  ] as const;
  // Grafana's RadioButtonList ignores falsy option values.
  const levelPrefix = 'level:';
  const levelValue = (severity: string) => `${levelPrefix}${severity}`;
  const selectedLevel = levelValue(draft.severity);
  const severityOptions = [
    { label: m.filters.severityAll, value: levelValue('') },
    ...levels.slice().sort(compareLevels).map((level) => ({ label: level.name[language], value: levelValue(level.id) })),
  ];
  if (!severityOptions.some(({ value }) => value === selectedLevel)) {
    severityOptions.push({ label: m.summary.retired(draft.severity), value: selectedLevel });
  }

  return (
    <form onSubmit={apply} className={styles.form} aria-label={m.filters.column(columnLabels(m)[column])}>
      {column === 'level' && (
        <RadioButtonList name="level" options={severityOptions} value={selectedLevel} className={styles.choices}
          onChange={(value) => change({ severity: value.slice(levelPrefix.length) })} />
      )}
      {column === 'name' && (
        <>
          <Field label={m.filters.titleContains}>
            <Input aria-label={m.filters.titleContains} maxLength={512} value={draft.title}
              onChange={(event) => change({ title: event.currentTarget.value })} />
          </Field>
          <Field label={m.filters.host}>
            <Input aria-label={m.filters.host} list="column-filter-hosts" maxLength={256} placeholder={m.filters.hostPlaceholder}
              value={draft.host} onChange={(event) => change({ host: event.currentTarget.value })} />
          </Field>
          <datalist id="column-filter-hosts">
            {facets?.hosts.map((host) => <option key={host} value={host} />)}
          </datalist>
        </>
      )}
      {column === 'status' && (
        <RadioButtonList<WorkspaceQuery['state']> name="status" options={statusOptions.slice()} value={draft.state}
          onChange={(state) => change({ state })} />
      )}
      {column === 'started' && (
        <>
          <Field label={`${m.range.from} (${zoneLabel})`}>
            <Input aria-label={m.range.from} type="datetime-local" step={1} value={from} onChange={(event) => { setFrom(event.currentTarget.value); setError(''); }} />
          </Field>
          <Field label={`${m.range.to} (${zoneLabel})`}>
            <Input aria-label={m.range.to} type="datetime-local" step={1} value={to} onChange={(event) => { setTo(event.currentTarget.value); setError(''); }} />
          </Field>
        </>
      )}
      {column === 'duration' && (
        <>
          <Field label={m.filters.durationFrom}>
            <Input aria-label={m.filters.durationFrom} type="number" min={0} step="any" value={min} onChange={(event) => { setMin(event.currentTarget.value); setError(''); }} />
          </Field>
          <Field label={m.filters.durationTo}>
            <Input aria-label={m.filters.durationTo} type="number" min={0} step="any" value={max} onChange={(event) => { setMax(event.currentTarget.value); setError(''); }} />
          </Field>
        </>
      )}
      {column === 'owner' && (
        <>
          <Field label={m.filters.assignment}>
            <RadioButtonList<WorkspaceQuery['assignment']> name="assignment" value={draft.assignment}
              options={[{ value: 'all', label: m.filters.assignmentAll }, { value: 'assigned', label: m.filters.assigned }, { value: 'unassigned', label: m.filters.unassigned }]}
              onChange={(assignment) => change({ assignment, ...(assignment === 'unassigned' ? { assignee: '' } : {}) })} />
          </Field>
          <Field label={m.filters.assignee}>
            <Input aria-label={m.filters.assignee} list="column-filter-assignees" maxLength={256}
              placeholder={m.filters.assigneePlaceholder} disabled={draft.assignment === 'unassigned'}
              value={draft.assignee} onChange={(event) => change({ assignee: event.currentTarget.value })} />
          </Field>
          <datalist id="column-filter-assignees">
            {facets?.assignees.map((owner) => <option key={owner} value={owner} />)}
          </datalist>
        </>
      )}
      {column === 'ack' && (
        <RadioButtonList<WorkspaceQuery['ack']> name="ack" value={draft.ack}
          options={[{ value: 'all', label: m.filters.ackAll }, { value: 'acknowledged', label: m.filters.acknowledged }, { value: 'unacknowledged', label: m.filters.unacknowledged }]}
          onChange={(ack) => change({ ack })} />
      )}
      {error && <div role="alert" className={styles.error}>{error}</div>}
      <div className={styles.actions}>
        <Button type="submit">{m.common.apply}</Button>
        <Button type="button" variant="secondary" onClick={() => { onFilter(clearColumnFilter(column)); close(); }}>{m.common.reset}</Button>
        {column === 'status' && (
          <Button type="button" variant="secondary" fill="text" onClick={() => { onSort('recovered'); close(); }}>{m.filters.sortRecovered}</Button>
        )}
      </div>
    </form>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  form: css`
    width: min(300px, calc(100vw - 72px));
    display: grid;
    gap: ${theme.spacing(1)};
    > div { min-width: 0; margin-bottom: 0; }
  `,
  actions: css`
    display: flex;
    flex-wrap: wrap;
    gap: ${theme.spacing(1)};
    margin-top: ${theme.spacing(0.5)};
  `,
  choices: css`
    max-height: min(320px, 50dvh);
    overflow-y: auto;
  `,
  error: css`
    color: ${theme.colors.error.text};
  `,
});
