// write-excel-file v4: the browser build, columns + toFile().
import writeXlsxFile from 'write-excel-file/browser';

export async function exportToExcel(data, fileHeader) {
  if (!data || data.length === 0) return;
  
  // Also truncate long strings for Excel export
  const excelData = data.map(item => {
    const processedItem = {};
    Object.keys(item).forEach(key => {
      const value = item[key];
      // Truncate any string longer than 1000 characters for Excel too
      if (typeof value === 'string' && value.length > 1000) {
        processedItem[key] = value.substring(0, 100) + '... [truncated]';
      } else {
        processedItem[key] = value;
      }
    });
    return processedItem;
  });

  const headers = Object.keys(excelData[0] || {});
  if (!headers.length) return;

  const columns = headers.map((header) => ({
    header,
    cell: (row) => {
      const value = row[header];
      if (value === null || value === undefined) return '';
      return typeof value === 'string' ? value : String(value);
    },
  }));

  await writeXlsxFile(excelData, { columns }).toFile(`${fileHeader}_${new Date().toISOString().slice(0, 10)}.xlsx`);
}
