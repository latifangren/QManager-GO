"use client";

import { useCallback, useRef, useState } from "react";
import { OnboardingShell } from "./onboarding-shell";
import { StepWelcome } from "./steps/step-welcome";
import { StepPassword } from "./steps/step-password";
import { StepNetworkMode } from "./steps/step-network-mode";
import { StepConnection } from "./steps/step-connection";
import { StepBandLocking } from "./steps/step-band-locking";
import { StepDone } from "./steps/step-done";

// =============================================================================
// OnboardingWizard — Orchestrates all 6 onboarding steps
// =============================================================================
// Steps:
//   1  Welcome       — static intro
//   2  Password      — required, calls setupPassword()
//   3  Network Mode  — optional, calls cellular/settings.sh
//   4  Connection    — optional, calls apn.sh or profiles/save.sh
//   5  Band Locking  — optional, calls bands/lock.sh
//   6  Done          — redirects to /dashboard/
// =============================================================================

// Per-step submit fns registered by each step component
type AsyncVoidFn = () => Promise<void>;
type SyncVoidFn = () => void;

export function OnboardingWizard() {
  const [currentStep, setCurrentStep] = useState(1);
  const [isLoading, setIsLoading] = useState(false);

  // Each step can register a submit fn. The shell's Continue button calls it.
  const submitFnRef = useRef<AsyncVoidFn | SyncVoidFn | null>(null);

  const advance = useCallback(() => {
    setCurrentStep((s) => Math.min(s + 1, 6));
  }, []);

  const goBack = useCallback(() => {
    setCurrentStep((s) => Math.max(s - 1, 1));
  }, []);

  // Shell's Continue button calls the step's registered submit fn.
  // Null the ref before awaiting to guard against double-click and
  // prevent a stale fn from the previous step firing during transitions.
  const handleContinue = useCallback(async () => {
    if (submitFnRef.current) {
      const fn = submitFnRef.current;
      submitFnRef.current = null;
      await fn();
    } else {
      advance();
    }
  }, [advance]);

  // Step 1 (Welcome) — no async, just advance
  const handleWelcomeContinue = useCallback(() => {
    advance();
  }, [advance]);

  return (
    <OnboardingShell
      currentStep={currentStep}
      onNext={currentStep === 1 ? handleWelcomeContinue : handleContinue}
      onBack={goBack}
      onSkip={currentStep >= 3 && currentStep <= 5 ? advance : undefined}
      isLoading={isLoading}
    >
      {currentStep === 1 && <StepWelcome />}

      {currentStep === 2 && (
        <StepPassword
          onSuccess={advance}
          onLoadingChange={setIsLoading}
          onSubmitRef={(fn) => {
            submitFnRef.current = fn;
          }}
        />
      )}

      {currentStep === 3 && (
        <StepNetworkMode
          onDataChange={() => {}}
          onSubmitRef={(fn) => {
            submitFnRef.current = fn;
          }}
          onLoadingChange={setIsLoading}
          onSuccess={advance}
        />
      )}

      {currentStep === 4 && (
        <StepConnection
          onSubmitRef={(fn) => {
            submitFnRef.current = fn;
          }}
          onLoadingChange={setIsLoading}
          onSuccess={advance}
        />
      )}

      {currentStep === 5 && (
        <StepBandLocking
          onSubmitRef={(fn) => {
            submitFnRef.current = fn;
          }}
          onLoadingChange={setIsLoading}
          onSuccess={advance}
        />
      )}

      {currentStep === 6 && <StepDone />}
    </OnboardingShell>
  );
}
