import { redirect } from "next/navigation";

export default function OnboardingConnectRedirect() {
  redirect("/servers?connect=1");
}
