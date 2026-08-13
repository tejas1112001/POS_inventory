import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { cn } from "@/lib/utils";

export function SectionCard({
  title,
  action,
  children,
  className,
  contentClassName,
}) {
  return (
    <Card className={cn("shadow-sm", className)}>
      {title && (
        <CardHeader className="flex flex-row items-center justify-between px-4 py-3 pb-2">
          <CardTitle className="text-sm font-semibold">{title}</CardTitle>
          {action}
        </CardHeader>
      )}
      <CardContent className={cn("p-4 pt-0", title && "pt-0", contentClassName)}>{children}</CardContent>
    </Card>
  );
}
