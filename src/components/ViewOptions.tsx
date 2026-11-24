import React, { type PropsWithChildren } from 'react';
import { Button, Checkbox, Stack, Toggletip } from '@grafana/ui';
import { useI18n } from '../i18n';
import { drawerSections, type ViewPreferences } from '../lib/preferences';

interface Props {
  scope: 'list' | 'drawer';
  preferences: ViewPreferences;
}

export function ViewOptions({ scope, preferences, children }: PropsWithChildren<Props>) {
  const { messages: { view, workflow } } = useI18n();
  const label = scope === 'list' ? view.title : view.more;
  const sections = scope === 'list' ? ['summary'] as const : drawerSections;
  return (
    <Toggletip title={label} placement="bottom-end" fitContent content={
      <Stack direction="column" gap={1.5}>
        {children}
        <Stack direction="column" alignItems="start" gap={0.5} role="group" aria-label={view.show}>
          {sections.map((section) => (
            <Checkbox key={section} label={section === 'workflow' ? workflow.title : view[section]} checked={preferences.visible.has(section)}
              onChange={(event) => preferences.setVisible(section, event.currentTarget.checked)} />
          ))}
        </Stack>
      </Stack>
    }>
      <Button type="button" size="sm" variant="secondary" fill="text" icon={scope === 'list' ? 'sliders-v-alt' : 'ellipsis-h'}>{label}</Button>
    </Toggletip>
  );
}
