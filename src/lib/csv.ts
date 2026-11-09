export function csvCell(value: string): string {
  const normalized = value.replace(/\r?\n/g, ' ');
  const defused = /^[=+\-@\t]/.test(normalized) ? `'${normalized}` : normalized;
  return `"${defused.replace(/"/g, '""')}"`;
}

export function toCSV(headers: string[], rows: string[][]): string {
  return [headers, ...rows].map((row) => row.map(csvCell).join(',')).join('\r\n');
}

export function downloadCSV(filename: string, content: string): void {
  const blob = new Blob([content], { type: 'text/csv;charset=utf-8' });
  const url = URL.createObjectURL(blob);
  const link = document.createElement('a');
  link.href = url;
  link.download = filename;
  link.click();
  URL.revokeObjectURL(url);
}
