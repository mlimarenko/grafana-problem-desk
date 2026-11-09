import { describe, expect, it } from 'vitest';
import { csvCell, toCSV } from './csv';

describe('CSV export', () => {
  it('quotes text and neutralizes spreadsheet formulas', () => {
    expect(csvCell('plain')).toBe('"plain"');
    expect(csvCell('say "hi"')).toBe('"say ""hi"""');
    expect(csvCell('=IMPORTXML("https://example.test")')).toBe(`"'=IMPORTXML(""https://example.test"")"`);
    expect(csvCell('+1')).toBe(`"'+1"`);
  });

  it('joins headers and rows with CRLF', () => {
    const result = toCSV(['a', 'b'], [['1', 'two\nlines']]);
    expect(result).toBe('"a","b"\r\n"1","two lines"');
  });
});
