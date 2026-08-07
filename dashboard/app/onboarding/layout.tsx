"use client";

import { usePathname } from "next/navigation";

const steps = [
  { name: "Connect Server", path: "/onboarding/connect-server" },
  { name: "Deploy App", path: "/onboarding/first-deploy" },
];

export default function OnboardingLayout({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const currentIdx = steps.findIndex((s) => pathname.startsWith(s.path));
  const step = currentIdx >= 0 ? currentIdx : 0;

  return (
    <div className="flex min-h-screen flex-col bg-[var(--color-paper)]">
      <header className="flex items-center gap-3 border-b border-[var(--color-border)] bg-[var(--color-surface)] px-6 py-4">
        <div className="flex h-9 w-9 items-center justify-center rounded-[var(--radius-md)] bg-[var(--color-accent)] text-sm font-extrabold text-[var(--color-accent-fg)]">
          A
        </div>
        <div>
          <span className="block text-base font-extrabold tracking-tight text-[var(--color-ink)]">
            Anchor
          </span>
          <span className="text-[10px] font-medium uppercase tracking-wider text-[var(--color-muted)]">
            Get started
          </span>
        </div>
      </header>

      <div className="border-b border-[var(--color-border)] bg-[var(--color-surface)] px-6 py-3">
        <div className="mx-auto flex max-w-2xl items-center gap-4">
          {steps.map((s, i) => (
            <div key={s.path} className="flex items-center gap-2">
              <div
                className={`flex h-6 w-6 items-center justify-center rounded-full text-xs font-bold ${
                  i < step
                    ? "bg-[var(--color-success)] text-white"
                    : i === step
                    ? "bg-[var(--color-accent)] text-[var(--color-accent-fg)]"
                    : "bg-[var(--color-paper-2)] text-[var(--color-muted)]"
                }`}
              >
                {i < step ? "✓" : i + 1}
              </div>
              <span
                className={`text-sm ${
                  i === step
                    ? "font-semibold text-[var(--color-ink)]"
                    : "text-[var(--color-muted)]"
                }`}
              >
                {s.name}
              </span>
              {i < steps.length - 1 && (
                <div className="mx-2 h-px w-8 bg-[var(--color-border)]" />
              )}
            </div>
          ))}
        </div>
      </div>

      <main className="flex flex-1 items-start justify-center px-4 py-10">
        <div className="w-full max-w-2xl">{children}</div>
      </main>
    </div>
  );
}
