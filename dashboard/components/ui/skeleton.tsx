export function Skeleton({ className, ...props }: React.HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={`animate-pulse rounded-[var(--radius-md)] bg-[var(--color-paper-2)] ${className || ""}`}
      {...props}
    />
  );
}
