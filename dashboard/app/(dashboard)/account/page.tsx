"use client";

import { useAuth } from "@/hooks/use-auth";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";

export default function AccountPage() {
  const { user } = useAuth();

  return (
    <div className="mx-auto max-w-lg space-y-6">
      <h1 className="text-2xl font-bold text-gray-900 dark:text-white">Account Settings</h1>
      <Card>
        <CardHeader>
          <CardTitle className="text-sm text-gray-500">Profile</CardTitle>
        </CardHeader>
        <CardContent className="space-y-3">
          <div>
            <div className="text-xs text-gray-400">Name</div>
            <div className="text-sm font-medium text-gray-900 dark:text-white">
              {user?.name || "—"}
            </div>
          </div>
          <div>
            <div className="text-xs text-gray-400">Email</div>
            <div className="text-sm font-medium text-gray-900 dark:text-white">
              {user?.email || "—"}
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
