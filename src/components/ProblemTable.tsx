import React from 'react';
import { css } from '@emotion/css';
import type { GrafanaTheme2 } from '@grafana/data';
import { Icon, useStyles2 } from '@grafana/ui';
import {
  elapsedMs,
  isAcknowledged,
  problemTitle,
  levelDisplay,
  levelColor,
  problemColumns,
  targetMetadata,
  targetName,
  type Problem,
  type LevelTone,
  type PolicyColumn,
} from '../model';
import { useI18n } from '../i18n';
import { sortByColumn } from '../lib/query';
import { columnLabels, ProblemColumnFilter, type ColumnFilterProps } from './ProblemColumnFilter';

interface Props extends ColumnFilterProps {
  problems: Problem[];
  selectedId: string;
  now: number;
  severityKey: string;
  columns: readonly PolicyColumn[];
  onOpen: (problem: Problem) => void;
}

const columnWidths: Record<PolicyColumn, number> = {
  level: 132, name: 190, status: 125, started: 139, duration: 148, owner: 157, ack: 159,
};

export function ProblemTable({ problems, selectedId, now, severityKey, columns, onOpen, ...filters }: Props) {
  const styles = useStyles2(getStyles);
  const { messages: m, language, formatDate, formatDuration, formatRelative } = useI18n();
  const { levels, onSort, query } = filters;
  const labels = columnLabels(m);
  const visibleColumns = problemColumns.filter((column) => columns.includes(column));

  return (
    <div className={styles.wrap} role="region" aria-label={m.table.caption} tabIndex={0}>
      <table className={styles.table} style={{ minWidth: visibleColumns.reduce((width, column) => width + columnWidths[column], 0) }}>
        <caption className={styles.srOnly}>{m.table.caption}</caption>
        <colgroup>
          {visibleColumns.map((column) => <col key={column} style={column === 'name' ? undefined : { width: columnWidths[column] }} />)}
        </colgroup>
        <thead>
          <tr>
            {visibleColumns.map((column) => {
              const sort = sortByColumn[column];
              const sorted = query.sort === sort || (column === 'status' && query.sort === 'recovered');
              return (
                <th key={column} scope="col" data-column={column}
                  aria-sort={sorted ? (query.order === 'asc' ? 'ascending' : 'descending') : undefined}>
                  <div className={styles.head}>
                    <button type="button" className={styles.sort} title={labels[column]} onClick={() => onSort(sort)}>
                      <span>{labels[column]}</span>
                      {sorted && <Icon name={query.order === 'asc' ? 'arrow-up' : 'arrow-down'} size="sm" />}
                    </button>
                    <ProblemColumnFilter column={column} {...filters} />
                  </div>
                </th>
              );
            })}
          </tr>
        </thead>
        <tbody>
          {problems.map((problem) => {
            const title = problemTitle(problem) || m.table.untitled;
            const target = targetName(problem);
            const metadata = targetMetadata(problem);
            const severity = levelDisplay(problem, levels, severityKey, language);
            const acknowledged = isAcknowledged(problem);
            const duration = elapsedMs(problem, now);
            const selected = problem.id === selectedId;
            return (
              <tr
                key={problem.id}
                className={selected ? styles.selectedRow : styles.row}
                onClick={() => onOpen(problem)}
                tabIndex={columns.includes('name') ? undefined : 0}
                aria-label={columns.includes('name') ? undefined : m.table.open(title)}
                onKeyDown={(event) => {
                  if (!columns.includes('name') && (event.key === 'Enter' || event.key === ' ')) {
                    event.preventDefault();
                    onOpen(problem);
                  }
                }}
                data-testid="problem-row"
              >
                {columns.includes('level') && <td>
                  <span className={styles.severityBadge(severity.tone)}>
                    <Icon
                      name={
                         severity.tone === 'red'
                          ? 'exclamation-triangle'
                           : severity.tone === 'orange' || severity.tone === 'yellow'
                            ? 'exclamation-circle'
                             : severity.tone === 'blue'
                              ? 'info-circle'
                              : 'circle'
                      }
                      size="sm"
                    />
                    {severity.label}
                  </span>
                </td>}
                {columns.includes('name') && <td>
                  <button
                    type="button"
                    className={styles.titleButton}
                    aria-label={m.table.open(title)}
                    aria-expanded={selected}
                    onClick={(event) => {
                      event.stopPropagation();
                      onOpen(problem);
                    }}
                  >
                    {title}
                  </button>
                  {((problem.labels.alertname && problem.labels.alertname !== title) || problem.labels.service) && (
                    <div className={styles.metaLine}>
                      {problem.labels.alertname && problem.labels.alertname !== title && <span>{problem.labels.alertname}</span>}
                      {problem.labels.service && <span>{problem.labels.service}</span>}
                    </div>
                  )}
                  <div className={styles.targetLine}>
                    <Icon name="monitor" size="sm" />
                    <span>{target || m.table.noTarget}</span>
                    {metadata.length > 0 && <span className={styles.metadata}>· {metadata.join(' · ')}</span>}
                  </div>
                </td>}
                {columns.includes('status') && <td>
                  <span className={styles.statusBadge(problem.status)}>
                    <span className={styles.statusDot(problem.status)} />
                    {problem.status === 'active' ? m.status.active : m.status.recovered}
                  </span>
                  {problem.status === 'recovered' && problem.endsAt && (
                    <div className={styles.secondary}>{m.table.recoveredAt(formatRelative(problem.endsAt, now))}</div>
                  )}
                </td>}
                {columns.includes('started') && <td>
                  <time dateTime={problem.startsAt} className={styles.started}>
                    {formatDate(problem.startsAt)}
                  </time>
                  <div className={styles.secondary}>{formatRelative(problem.startsAt, now)}</div>
                </td>}
                {columns.includes('duration') && <td className={styles.duration}>{duration === undefined ? '—' : formatDuration(duration)}</td>}
                {columns.includes('owner') && <td>
                  <span className={problem.assignee ? styles.owner : styles.unassigned}>
                    {problem.assignee || m.filters.unassigned}
                  </span>
                </td>}
                {columns.includes('ack') && <td>
                  {acknowledged ? (
                    <span className={styles.ackBadge} title={m.ack.acknowledgedBy(problem.acknowledgedBy)}>
                      <Icon name="check" size="sm" />
                      {m.ack.acknowledged}
                      <span className={styles.ackAuthor}>{problem.acknowledgedBy}</span>
                    </span>
                  ) : (
                    <span className={styles.muted}>{m.ack.notAcknowledged}</span>
                  )}
                </td>}
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

const getStyles = (theme: GrafanaTheme2) => ({
  wrap: css`
    min-width: 0;
    overflow-x: auto;
    overscroll-behavior-inline: contain;
  `,
  table: css`
    width: 100%;
    table-layout: fixed;
    border-collapse: collapse;
    font-size: ${theme.typography.body.fontSize};
    th,
    td {
      padding: ${theme.spacing(0.75)} ${theme.spacing(1)};
      border-bottom: 1px solid ${theme.colors.border.weak};
      text-align: left;
      vertical-align: top;
    }
    th {
      position: sticky;
      top: 0;
      z-index: 1;
      padding-inline: ${theme.spacing(0.75)};
      background: ${theme.colors.background.secondary};
      border: 1px solid ${theme.colors.border.weak};
      border-top: 0;
      color: ${theme.colors.text.secondary};
      font-size: 0.93em;
      font-weight: ${theme.typography.fontWeightMedium};
      white-space: nowrap;
      &:first-child { border-left: 0; }
      &:last-child { border-right: 0; }
    }
    tbody tr:last-child td {
      border-bottom: 0;
    }
  `,
  head: css`
    display: flex;
    align-items: center;
    justify-content: space-between;
    min-height: ${theme.spacing(3)};
    gap: ${theme.spacing(0.5)};
  `,
  sort: css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    min-width: 0;
    min-height: ${theme.spacing(3)};
    padding: 0;
    border: 0;
    background: transparent;
    color: inherit;
    font: inherit;
    cursor: pointer;
    > span:first-child { overflow: hidden; text-overflow: ellipsis; }
    > svg { flex-shrink: 0; }
    &:hover { color: ${theme.colors.text.primary}; }
    &:focus-visible {
      outline: 2px solid ${theme.colors.primary.border};
      outline-offset: 1px;
    }
  `,
  row: css`
    cursor: pointer;
    transition: background 120ms ease;
    &:hover {
      background: ${theme.colors.action.hover};
    }
    &:focus-within {
      outline: 2px solid ${theme.colors.primary.border};
      outline-offset: -2px;
    }
  `,
  selectedRow: css`
    cursor: pointer;
    background: ${theme.colors.action.selected};
    &:hover {
      background: ${theme.colors.action.selected};
    }
    &:focus-within {
      outline: 2px solid ${theme.colors.primary.border};
      outline-offset: -2px;
    }
  `,
  titleButton: css`
    display: block;
    width: 100%;
    padding: 0;
    border: 0;
    background: none;
    color: ${theme.colors.text.primary};
    font: inherit;
    font-weight: ${theme.typography.fontWeightMedium};
    text-align: left;
    overflow-wrap: anywhere;
    cursor: pointer;
    &:hover {
      color: ${theme.colors.text.link};
      text-decoration: underline;
    }
    &:focus-visible {
      outline: 2px solid ${theme.colors.primary.border};
      outline-offset: 2px;
    }
  `,
  metaLine: css`
    display: flex;
    flex-wrap: wrap;
    gap: ${theme.spacing(1)};
    margin-top: 2px;
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
  `,
  targetLine: css`
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: ${theme.spacing(0.5)};
    margin-top: 2px;
    color: ${theme.colors.text.secondary};
    overflow-wrap: anywhere;
  `,
  metadata: css`
    color: ${theme.colors.text.secondary};
  `,
  secondary: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin-top: 2px;
    white-space: nowrap;
  `,
  started: css`
    font-variant-numeric: tabular-nums;
  `,
  duration: css`
    white-space: nowrap;
    font-variant-numeric: tabular-nums;
  `,
  severityBadge: (tone: LevelTone) => css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    padding: 2px ${theme.spacing(1)};
    border: 1px solid ${levelColor(theme, tone)};
    border-radius: ${theme.shape.radius.default};
    color: ${levelColor(theme, tone)};
    font-size: ${theme.typography.bodySmall.fontSize};
    overflow-wrap: anywhere;
    font-weight: ${theme.typography.fontWeightMedium};
  `,
  statusBadge: (status: Problem['status']) => css`
    display: inline-flex;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    color: ${status === 'active' ? theme.colors.error.text : theme.colors.success.text};
    font-weight: ${theme.typography.fontWeightMedium};
    white-space: nowrap;
  `,
  statusDot: (status: Problem['status']) => css`
    width: 7px;
    height: 7px;
    border-radius: 50%;
    background: ${status === 'active' ? theme.colors.error.main : theme.colors.success.main};
  `,
  owner: css`
    overflow-wrap: anywhere;
  `,
  unassigned: css`
    color: ${theme.colors.text.secondary};
  `,
  ackBadge: css`
    display: inline-flex;
    align-items: center;
    flex-wrap: wrap;
    gap: ${theme.spacing(0.5)};
    color: ${theme.colors.success.text};
  `,
  ackAuthor: css`
    color: ${theme.colors.text.secondary};
    overflow-wrap: anywhere;
  `,
  muted: css`
    color: ${theme.colors.text.secondary};
  `,
  srOnly: css`
    position: absolute;
    width: 1px;
    height: 1px;
    padding: 0;
    margin: -1px;
    overflow: hidden;
    clip: rect(0, 0, 0, 0);
    white-space: nowrap;
    border: 0;
  `,
});
