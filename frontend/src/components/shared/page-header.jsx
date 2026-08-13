import { ChevronRight } from "lucide-react";
import { Link } from "react-router-dom";
import { cn } from "@/lib/utils";

export function PageHeader({
  title,
  breadcrumbs,
  actions,
  description,
  className,
  hideTitle = false,
}) {
  return (
    <div className={cn("flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between", className)}>
      <div>
        {breadcrumbs && breadcrumbs.length > 0 && (
          <nav className="mb-0.5 flex items-center text-xs text-muted-foreground" aria-label="Breadcrumb">
            {breadcrumbs.map((b, i) => (
              <span key={i} className="flex items-center">
                {i > 0 && <ChevronRight className="mx-1 size-3" />}
                {b.to ? (
                  <Link
                    to={b.to}
                    className="hover:text-foreground transition-colors"
                  >
                    {b.label}
                  </Link>
                ) : (
                  <span className="text-foreground font-medium">{b.label}</span>
                )}
              </span>
            ))}
          </nav>
        )}
        {!hideTitle && title && (
          <h1 className="text-lg font-bold tracking-tight text-foreground sm:text-xl">{title}</h1>
        )}
        {description && <p className="text-xs text-muted-foreground">{description}</p>}
      </div>
      {actions && <div className="flex items-center gap-2">{actions}</div>}
    </div>
  );
}
