"use client";

import LatencyMonitoringCard, {
  useLatencyMonitoring,
} from "./latency-monitoring-card";
import PingEntriesCard from "./ping-entries-card";

const LatencyMonitoringComponent = () => {
  const { viewMode, setViewMode, chartData, total, tableData } =
    useLatencyMonitoring();

  return (
    <div className="@container/main mx-auto p-2">
      <div className="mb-6">
        <h1 className="text-3xl font-bold mb-2">Latency Monitoring</h1>
        <p className="text-muted-foreground">
          Monitor and analyze latency and packet loss to identify potential
          issues and optimize performance.
        </p>
      </div>
      <div className="grid grid-cols-1 @3xl/main:grid-cols-2 grid-flow-row gap-4">
        <LatencyMonitoringCard
          viewMode={viewMode}
          setViewMode={setViewMode}
          chartData={chartData}
          total={total}
        />
        <PingEntriesCard
          entries={tableData.entries}
          emptyMessage={tableData.emptyMessage}
          isRealtime={tableData.isRealtime}
        />
      </div>
    </div>
  );
};

export default LatencyMonitoringComponent;
