export default function OnboardingLayout({ children }: { children: React.ReactNode }) {
  return (
    <div className="flex min-h-screen items-center justify-center bg-gray-50 px-4 dark:bg-gray-950">
      <div className="w-full max-w-2xl space-y-8">{children}</div>
    </div>
  );
}
