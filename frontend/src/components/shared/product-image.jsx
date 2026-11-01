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
  const initialsBox = (
    <div
      className={cn(
        "flex items-center justify-center font-semibold text-white shrink-0",
        !color && "bg-primary",
        rounded,
        className
      )}
      style={{
        backgroundColor: color,
        width: size,
        height: size,
        fontSize: Math.max(10, size * 0.28),
      }}
      title={name}
    >
      {initials(name).slice(0, 2) || "PR"}
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
