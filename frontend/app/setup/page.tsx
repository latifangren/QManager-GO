"use client";

import { useEffect, useState } from "react";
import { isLoggedIn } from "@/hooks/use-auth";
import { OnboardingWizard } from "@/components/onboarding/onboarding-wizard";
import { Spinner } from "@/components/ui/spinner";

// =============================================================================
// /setup — First-time onboarding wizard route
// =============================================================================
// Guards:
//   - Already logged in + onboarding completed → redirect /dashboard/
//   - setup_required is false (password already set, not logged in) → /login/
// Renders OnboardingWizard when setup_required is confirmed.
// =============================================================================

const CHECK_ENDPOINT = "/cgi-bin/quecmanager/auth/check.sh";

export default function SetupPage() {
  const [ready, setReady] = useState(false);

  useEffect(() => {
    // Guard 1: already logged in
    if (isLoggedIn()) {
      window.location.href = "/dashboard/";
      return;
    }

    // Guard 2: confirm setup_required via backend (retry on failure —
    // during first boot lighttpd may start before the backend is ready)
    let attempt = 0;
    const maxRetries = 3;
    const retryDelay = 1500; // ms

    function checkSetup() {
      fetch(CHECK_ENDPOINT)
        .then((r) => r.json())
        .then((data) => {
          if (!data.setup_required) {
            // Password already set — go to normal login
            window.location.href = "/login/";
            return;
          }
          setReady(true);
        })
        .catch(() => {
          attempt++;
          if (attempt < maxRetries) {
            setTimeout(checkSetup, retryDelay);
          } else {
            // Backend still unreachable after retries — show wizard anyway.
            // On a fresh install this is the safe default.
            setReady(true);
          }
        });
    }

    checkSetup();
  }, []);

  if (!ready) {
    return (
      <div className="flex min-h-svh items-center justify-center bg-background">
        <Spinner className="size-6 text-muted-foreground" />
      </div>
    );
  }

  return <OnboardingWizard />;
}
