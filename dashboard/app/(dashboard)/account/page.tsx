"use client";

import { useAuth } from "@/hooks/use-auth";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { TeamSection } from "@/components/account/team-section";

export default function AccountPage() {
  const { user } = useAuth();

  return (
    <div className="mx-auto max-w-lg space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="text-sm font-semibold uppercase tracking-wider text-[var(--color-muted)]">
            Profile
          </CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div>
            <div className="text-xs font-medium text-[var(--color-muted)]">Name</div>
            <div className="text-sm font-semibold text-[var(--color-ink)]">
              {user?.name || "—"}
            </div>
          </div>
          <div>
            <div className="text-xs font-medium text-[var(--color-muted)]">Email</div>
            <div className="text-sm font-semibold text-[var(--color-ink)]">
              {user?.email || "—"}
            </div>
          </div>
        </CardContent>
      </Card>

      <TeamSection />
    </div>
  );
}
