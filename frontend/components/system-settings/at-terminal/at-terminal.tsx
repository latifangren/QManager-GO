"use client";

import ATTerminalCard from "@/components/system-settings/at-terminal/at-terminal-card";

const ATTerminal = () => {
  return (
    <div className="@container/main mx-auto p-2">
      <div className="mb-6">
        <h1 className="text-3xl font-bold mb-2">AT Terminal</h1>
        <p className="text-muted-foreground">
          Send AT commands directly to the modem.
        </p>
      </div>
      <ATTerminalCard />
    </div>
  );
};

export default ATTerminal;
