// PageHeader: the page's actions, at the right. The page title is in the
// top bar and pages carry no intro line, so it renders nothing without
// actions or children.
export default function PageHeader({ actions, children }) {
  if (!actions && !children) return null;
  return (
    <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
      <div className="min-w-0">{children}</div>
      {actions && <div className="flex flex-wrap items-center gap-2 max-sm:w-full max-sm:[&>*]:flex-1">{actions}</div>}
    </div>
  );
}
