import { DropdownRangeDatePicker } from '@/components/ui/dropdown-range-date-picker';

// DateRangePicker: the date filter of the report pages (one range button).
export default function DateRangePicker({ fromDate, toDate, setFromDate, setToDate }) {
  return (
    <div className="md:col-span-3">
      <span className="mb-1 block text-sm font-medium text-gray-700">Date</span>
      <DropdownRangeDatePicker
        className="w-full"
        placeholder="All dates"
        value={{ from: fromDate, to: toDate }}
        onChange={({ from, to }) => {
          setFromDate(from);
          setToDate(to);
        }}
      />
    </div>
  );
}
