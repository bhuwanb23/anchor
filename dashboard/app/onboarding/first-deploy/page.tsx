import { redirect } from "next/navigation";

export default function OnboardingFirstDeployRedirect() {
  redirect("/servers");
}
