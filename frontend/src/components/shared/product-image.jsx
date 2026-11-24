import { Package } from "lucide-react";
import { initials } from "@/lib/format";
import { cn } from "@/lib/utils";
import { AuthImage } from "./auth-image";

export function ProductImage({
  name,
  color,
  src,
  alt,
  size = 40,
  className,
  rounded = "rounded-md",
}) {
  // No picture: a soft teal tile with the initials; large tiles (the
  // checkout grid passes size 0) also show a product icon.
  const large = size === 0 || size >= 64;
  const initialsBox = (
    <div
      className={cn(
        "flex shrink-0 flex-col items-center justify-center gap-1 font-bold",
        color ? "text-white" : "bg-teal-soft bg-gradient-to-br from-transparent to-[color-mix(in_oklab,var(--ds-teal)_14%,transparent)] text-primary dark:text-teal",
        rounded,
        className
      )}
      style={{
        backgroundColor: color,
        width: size || undefined,
        height: size || undefined,
        fontSize: large ? 12 : Math.max(10, size * 0.3),
      }}
      title={name}
    >
      {large && <Package className="size-9 opacity-80" strokeWidth={1.5} aria-hidden="true" />}
      <span className={large ? "tracking-wider opacity-80" : undefined}>{initials(name).slice(0, 2) || "PR"}</span>
    </div>
  );

  if (!src) return initialsBox;
  return (
    <AuthImage
      src={src}
      alt={alt || name}
      title={name}
      className={cn("shrink-0 object-cover", rounded, className)}
      style={size ? { width: size, height: size } : undefined}
      fallback={initialsBox}
    />
  );
}
