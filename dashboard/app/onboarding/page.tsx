import { redirect } from "next/navigation";

/** Onboarding wizard removed — connect lives on /servers?connect=1 */
export default function OnboardingIndexPage() {
  redirect("/servers?connect=1");
}
