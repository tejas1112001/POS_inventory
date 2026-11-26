import { Package } from "lucide-react";
import { initials } from "@/lib/format";
import { cn } from "@/lib/utils";
import { AuthImage } from "./auth-image";

// No-picture tiles get one of a few soft tones picked from the name, so
// neighbouring products without photos look different at a glance.
const TONES = [
  "bg-teal-soft text-primary dark:text-teal",
  "bg-amber-soft text-[#8a5300] dark:text-amber-brand",
  "bg-[#e3eef6] text-[#24557a] dark:bg-[#123447] dark:text-[#9ccbea]",
  "bg-[#ece8f5] text-[#4f3f86] dark:bg-[#2a2747] dark:text-[#c6bdf0]",
];
const toneFor = (name = "") => TONES[[...name].reduce((h, ch) => (h * 31 + ch.charCodeAt(0)) >>> 0, 7) % TONES.length];

export function ProductImage({
  name,
  color,
  src,
  alt,
  size = 40,
  className,
  rounded = "rounded-md",
}) {
  // No picture: a soft tile with the initials; large tiles (the checkout
  // grid passes size 0) show a product icon with the initials under it.
  const large = size === 0 || size >= 64;
  const initialsBox = (
    <div
      className={cn(
        "flex shrink-0 flex-col items-center justify-center gap-1 font-bold",
        color ? "text-white" : toneFor(name),
        rounded,
        className
      )}
      style={{
        backgroundColor: color,
        width: size || undefined,
        height: size || undefined,
        fontSize: large ? 14 : Math.max(10, size * 0.3),
      }}
      title={name}
    >
      {large && <Package className="size-10 opacity-70" strokeWidth={1.4} aria-hidden="true" />}
      <span className={large ? "tracking-wider opacity-90" : undefined}>{initials(name).slice(0, 2) || "PR"}</span>
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
