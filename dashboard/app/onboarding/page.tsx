"use client";

import { useEffect } from "react";
import { useRouter } from "next/navigation";

export default function OnboardingPage() {
  const router = useRouter();
  useEffect(() => {
    router.replace("/onboarding/connect-server");
  }, [router]);
  return null;
}
