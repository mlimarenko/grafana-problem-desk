import React from 'react';
import { css } from '@emotion/css';
import type { GrafanaTheme2 } from '@grafana/data';
import { Icon, useStyles2 } from '@grafana/ui';
import { useI18n } from '../i18n';
import { levelColor, type LevelTone, type Problem } from '../model';

export function SeverityBadge({ label, tone }: { label: string; tone: LevelTone }) {
  const styles = useStyles2(getStyles);
  const icon = tone === 'red' ? 'exclamation-triangle' : tone === 'orange' || tone === 'yellow' ? 'exclamation-circle' : tone === 'blue' ? 'info-circle' : 'circle';
  return <span className={styles.level(tone)}><Icon name={icon} size="sm" />{label}</span>;
}

export function ProblemStatus({ status }: { status: Problem['status'] }) {
  const styles = useStyles2(getStyles);
  const { messages: m } = useI18n();
  return <span className={styles.status(status)}>{status === 'active' ? m.status.active : m.status.recovered}</span>;
}

const getStyles = (theme: GrafanaTheme2) => ({
  level: (tone: LevelTone) => css`
    display: inline-flex;
    max-width: 100%;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    padding: 2px ${theme.spacing(1)};
    border: 1px solid currentColor;
    border-radius: ${theme.shape.radius.default};
    color: ${levelColor(theme, tone)};
    font-size: ${theme.typography.bodySmall.fontSize};
    font-weight: ${theme.typography.fontWeightMedium};
    overflow-wrap: anywhere;
  `,
  status: (status: Problem['status']) => css`
    display: inline-flex;
    max-width: 100%;
    align-items: center;
    gap: ${theme.spacing(0.5)};
    color: ${status === 'active' ? theme.colors.error.text : theme.colors.success.text};
    font-weight: ${theme.typography.fontWeightMedium};
    overflow-wrap: anywhere;
    &::before {
      content: '';
      width: 7px;
      height: 7px;
      flex-shrink: 0;
      border-radius: 50%;
      background: ${status === 'active' ? theme.colors.error.main : theme.colors.success.main};
    }
  `,
});
