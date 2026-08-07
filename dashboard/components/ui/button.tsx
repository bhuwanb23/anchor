import { type ButtonHTMLAttributes, forwardRef } from "react";

interface ButtonProps extends ButtonHTMLAttributes<HTMLButtonElement> {
  variant?: "primary" | "secondary" | "ghost" | "danger";
  size?: "sm" | "md" | "lg";
}

const Button = forwardRef<HTMLButtonElement, ButtonProps>(
  ({ className = "", variant = "primary", size = "md", children, ...props }, ref) => {
    const base =
      "inline-flex items-center justify-center font-semibold tracking-tight transition-[transform,background-color,box-shadow,color] duration-[var(--dur-med)] ease-[var(--ease-out)] focus:outline-none focus-visible:ring-2 focus-visible:ring-[var(--color-focus)] focus-visible:ring-offset-2 disabled:opacity-50 disabled:pointer-events-none active:translate-y-px";

    const variants = {
      primary:
        "bg-[var(--color-accent)] text-[var(--color-accent-fg)] hover:bg-[var(--color-accent-hover)] shadow-sm hover:shadow-[var(--shadow-lift)]",
      secondary:
        "bg-[var(--color-surface)] text-[var(--color-ink)] border border-[var(--color-border)] hover:bg-[var(--color-paper-2)]",
      ghost:
        "text-[var(--color-muted)] hover:bg-[var(--color-accent-soft)] hover:text-[var(--color-accent)]",
      danger:
        "bg-[var(--color-danger)] text-white hover:opacity-90",
    };

    const sizes = {
      sm: "rounded-[var(--radius-sm)] px-3 py-1.5 text-sm",
      md: "rounded-[var(--radius-md)] px-4 py-2.5 text-sm",
      lg: "rounded-[var(--radius-md)] px-6 py-3 text-base",
    };

    return (
      <button
        ref={ref}
        className={`${base} ${variants[variant]} ${sizes[size]} ${className}`}
        {...props}
      >
        {children}
      </button>
    );
  }
);

Button.displayName = "Button";

export { Button };
export type { ButtonProps };
