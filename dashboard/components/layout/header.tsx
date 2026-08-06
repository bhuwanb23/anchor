"use client";

import NotificationCenter from "@/components/dashboard/notification-center";

export function Header() {
  return (
    <header className="flex h-14 items-center justify-between border-b bg-white px-6 dark:border-gray-800 dark:bg-gray-900">
      <div />
      <NotificationCenter />
    </header>
  );
}
