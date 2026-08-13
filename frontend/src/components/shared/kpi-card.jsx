import { Card, CardContent } from "@/components/ui/card";
import { ArrowDownRight, ArrowUpRight } from "lucide-react";
import { cn } from "@/lib/utils";

export function KpiCard({
  label,
  value,
  trend,
  trendLabel,
  icon: Icon,
  accent = "emerald",
}) {
  const accentBg = {
    emerald: "bg-emerald-50 text-emerald-700",
    amber: "bg-amber-50 text-amber-700",
    sky: "bg-sky-50 text-sky-700",
    rose: "bg-rose-50 text-rose-700",
    violet: "bg-violet-50 text-violet-700",
  };
  const positive = (trend ?? 0) >= 0;
  return (
    <Card className="shadow-sm hover:shadow-md transition-shadow">
      <CardContent className="p-4">
        <div className="flex items-start justify-between">
          <div>
            <p className="text-xs text-muted-foreground">{label}</p>
            <p className="mt-1 text-xl font-bold tracking-tight text-foreground">{value}</p>
          </div>
          <div className={cn("flex size-8 items-center justify-center rounded-md", accentBg[accent])}>
            <Icon className="size-4" />
          </div>
        </div>
        {trend !== undefined && (
          <div className="mt-2 flex items-center gap-1.5 text-xs">
            <span
              className={cn(
                "inline-flex items-center gap-0.5 rounded-md px-1.5 py-0.5 font-medium",
                positive ? "bg-emerald-50 text-emerald-700" : "bg-rose-50 text-rose-700"
              )}
            >
              {positive ? <ArrowUpRight className="size-3" /> : <ArrowDownRight className="size-3" />}
              {Math.abs(trend)}%
            </span>
            <span className="text-muted-foreground">{trendLabel || "vs last period"}</span>
          </div>
        )}
      </CardContent>
    </Card>
  );
}
