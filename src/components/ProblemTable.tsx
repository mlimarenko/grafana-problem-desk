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
  targetMetadata,
  targetName,
  type Problem,
  type Level,
  type LevelTone,
  type PolicyColumn,
} from '../model';
import { useI18n } from '../i18n';

interface Props {
  problems: Problem[];
  selectedId: string;
  now: number;
  levels: Level[];
  severityKey: string;
  columns: PolicyColumn[];
  onOpen: (problem: Problem) => void;
}

export function ProblemTable({ problems, selectedId, now, levels, severityKey, columns, onOpen }: Props) {
  const styles = useStyles2(getStyles);
  const { messages: m, language, formatDate, formatDuration, formatRelative } = useI18n();

  return (
    <div className={styles.wrap}>
      <table className={styles.table}>
        <caption className={styles.srOnly}>{m.table.caption}</caption>
        <thead>
          <tr>
            {columns.includes('level') && <th scope="col">{m.table.severity}</th>}
            {columns.includes('name') && <th scope="col">{m.table.problem}</th>}
            {columns.includes('status') && <th scope="col">{m.table.status}</th>}
            {columns.includes('started') && <th scope="col">{m.table.started}</th>}
            {columns.includes('duration') && <th scope="col">{m.table.duration}</th>}
            {columns.includes('owner') && <th scope="col">{m.table.owner}</th>}
            {columns.includes('ack') && <th scope="col">{m.table.ack}</th>}
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
                {columns.includes('name') && <td className={styles.problemCell}>
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
                  <div className={styles.metaLine}>
                    {problem.labels.alertname && problem.labels.alertname !== title && <span>{problem.labels.alertname}</span>}
                    {problem.labels.service && <span>{problem.labels.service}</span>}
                  </div>
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
  `,
  table: css`
    width: 100%;
    border-collapse: collapse;
    font-size: ${theme.typography.bodySmall.fontSize};
    th,
    td {
      padding: ${theme.spacing(1)} ${theme.spacing(1.5)};
      border-bottom: 1px solid ${theme.colors.border.weak};
      text-align: left;
      vertical-align: top;
    }
    th {
      position: sticky;
      top: 0;
      z-index: 1;
      background: ${theme.colors.background.primary};
      border-bottom: 1px solid ${theme.colors.border.medium};
      color: ${theme.colors.text.secondary};
      font-weight: ${theme.typography.fontWeightMedium};
      white-space: nowrap;
    }
    th:nth-child(1) {
      width: 112px;
    }
    th:nth-child(3),
    th:nth-child(5) {
      width: 118px;
    }
    th:nth-child(4) {
      width: 158px;
    }
    th:nth-child(6) {
      width: 150px;
    }
    th:nth-child(7) {
      width: 188px;
    }
    tbody tr:last-child td {
      border-bottom: 0;
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
  problemCell: css`
    min-width: 260px;
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
    color: ${theme.colors.text.disabled};
  `,
  secondary: css`
    color: ${theme.colors.text.secondary};
    font-size: ${theme.typography.bodySmall.fontSize};
    margin-top: 2px;
    white-space: nowrap;
  `,
  started: css`
    white-space: nowrap;
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
    border-radius: 999px;
    color: ${levelColor(theme, tone)};
    white-space: nowrap;
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
