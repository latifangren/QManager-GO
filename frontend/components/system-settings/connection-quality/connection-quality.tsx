"use client";

import ConnectivitySensitivityCard from "@/components/system-settings/connection-quality/connectivity-sensitivity-card";
import QualityThresholdsCard from "@/components/system-settings/connection-quality/quality-thresholds-card";

const ConnectionQualitySettings = () => {
  return (
    <div className="@container/main mx-auto p-2">
      <div className="mb-6">
        <h1 className="text-3xl font-bold mb-2">Connection Quality</h1>
        <p className="text-muted-foreground">
          Probe sensitivity, and when latency or packet loss is flagged as an event.
        </p>
      </div>
      <div className="grid grid-cols-1 @3xl/main:grid-cols-2 grid-flow-row gap-4">
        <ConnectivitySensitivityCard />
        <QualityThresholdsCard />
      </div>
    </div>
  );
};

export default ConnectionQualitySettings;
