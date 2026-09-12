"use client";

import React, { useMemo, useState } from "react";
import { motion } from "motion/react";
import { toast } from "sonner";
import {
  ActivityIcon,
  ArrowDownIcon,
  ArrowUpIcon,
  CalendarIcon,
  ClockIcon,
  HardDriveIcon,
  NetworkIcon,
  RefreshCwIcon,
  RotateCcwIcon,
  TrendingUpIcon,
} from "lucide-react";
import {
  Area,
  AreaChart,
  Bar,
  BarChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";

import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table";
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";

import { useBandwidth, formatBps } from "@/hooks/use-bandwidth";
import { formatBytes } from "@/types/modem-status";
import { staggerContainer, staggerItem } from "@/lib/motion";
import { cn } from "@/lib/utils";

import {
  CARD_DESC,
  CARD_PAD,
  CARD_SHELL,
  CARD_TITLE,
  FOCUS_RING,
  PAGE_HEAD,
  PAGE_ROOT,
  PILL_ACTION,
  SUMMARY_GRID,
} from "./shapes";

// Helper for converting bps to Mbps number with 2 decimals
function bpsToMbps(bps: number): number {
  if (!bps || isNaN(bps) || bps < 0) return 0;
  return Number((bps / 1_000_000).toFixed(2));
}

export default function BandwidthPage() {
  const {
    data,
    currentIface,
    interfaceNames,
    selectedIface,
    setSelectedIface,
    isLoading,
    isResetting,
    refresh,
    resetCounters,
  } = useBandwidth();

  const [resetDialogOpen, setResetDialogOpen] = useState(false);
  const [activeTab, setActiveTab] = useState("24h");

  const handleRefresh = async () => {
    await refresh();
    toast.success("Bandwidth data refreshed");
  };

  const handleResetConfirm = async () => {
    const success = await resetCounters(selectedIface);
    if (success) {
      toast.success(`Reset traffic counters for ${selectedIface}`);
    } else {
      toast.error("Failed to reset traffic counters");
    }
    setResetDialogOpen(false);
  };

  // Extract metrics for current interface
  const rxBps = currentIface?.current_rx_bps ?? 0;
  const txBps = currentIface?.current_tx_bps ?? 0;
  const todayRx = currentIface?.today_rx_bytes ?? 0;
  const todayTx = currentIface?.today_tx_bytes ?? 0;
  const todayTotal = todayRx + todayTx;

  // Realtime 60s chart data
  const realtimeChartData = useMemo(() => {
    if (!currentIface?.realtime || currentIface.realtime.length === 0) {
      return Array.from({ length: 30 }, (_, i) => ({
        time: `-${30 - i}s`,
        download: 0,
        upload: 0,
        rawRx: 0,
        rawTx: 0,
      }));
    }

    const points = currentIface.realtime;
    return points.map((p, idx) => {
      const secondsAgo = points.length - 1 - idx;
      return {
        time: secondsAgo === 0 ? "Now" : `-${secondsAgo}s`,
        download: bpsToMbps(p.rx_bps),
        upload: bpsToMbps(p.tx_bps),
        rawRx: p.rx_bps,
        rawTx: p.tx_bps,
      };
    });
  }, [currentIface?.realtime]);

  // Peak rates in last 60s
  const peakRx = useMemo(() => {
    if (!currentIface?.realtime || currentIface.realtime.length === 0) return rxBps;
    return Math.max(...currentIface.realtime.map((p) => p.rx_bps), rxBps);
  }, [currentIface?.realtime, rxBps]);

  const peakTx = useMemo(() => {
    if (!currentIface?.realtime || currentIface.realtime.length === 0) return txBps;
    return Math.max(...currentIface.realtime.map((p) => p.tx_bps), txBps);
  }, [currentIface?.realtime, txBps]);

  // Yesterday usage
  const yesterdayData = useMemo(() => {
    const daily = currentIface?.daily ?? [];
    if (daily.length >= 2) {
      const item = daily[daily.length - 2];
      return {
        date: item.date,
        rx: item.rx_bytes,
        tx: item.tx_bytes,
        total: item.rx_bytes + item.tx_bytes,
      };
    }
    return { date: "-", rx: 0, tx: 0, total: 0 };
  }, [currentIface?.daily]);

  // Last 7 days total & avg
  const last7DaysData = useMemo(() => {
    const daily = currentIface?.daily ?? [];
    const slice = daily.slice(-7);
    if (slice.length === 0) return { total: 0, rx: 0, tx: 0, avg: 0, count: 0 };

    let totalRx = 0;
    let totalTx = 0;
    slice.forEach((d) => {
      totalRx += d.rx_bytes;
      totalTx += d.tx_bytes;
    });
    const total = totalRx + totalTx;
    return {
      total,
      rx: totalRx,
      tx: totalTx,
      avg: slice.length > 0 ? total / slice.length : 0,
      count: slice.length,
    };
  }, [currentIface?.daily]);

  // This month usage
  const thisMonthData = useMemo(() => {
    const monthly = currentIface?.monthly ?? [];
    if (monthly.length > 0) {
      const current = monthly[monthly.length - 1];
      return {
        month: current.month,
        rx: current.rx_bytes,
        tx: current.tx_bytes,
        total: current.rx_bytes + current.tx_bytes,
      };
    }
    return { month: "-", rx: 0, tx: 0, total: 0 };
  }, [currentIface?.monthly]);

  // Hourly chart & table data (24h)
  const hourlyData = useMemo(() => {
    const raw = currentIface?.hourly ?? [];
    return raw.map((h) => {
      const rxMb = Number((h.rx_bytes / 1_048_576).toFixed(1));
      const txMb = Number((h.tx_bytes / 1_048_576).toFixed(1));
      return {
        label: h.hour.split(" ")[1] || h.hour,
        fullHour: h.hour,
        downloadMb: rxMb,
        uploadMb: txMb,
        downloadBytes: h.rx_bytes,
        uploadBytes: h.tx_bytes,
        totalBytes: h.rx_bytes + h.tx_bytes,
      };
    });
  }, [currentIface?.hourly]);

  // Daily chart & table data (30d)
  const dailyData = useMemo(() => {
    const raw = currentIface?.daily ?? [];
    return raw.map((d) => {
      const rxMb = Number((d.rx_bytes / 1_048_576).toFixed(1));
      const txMb = Number((d.tx_bytes / 1_048_576).toFixed(1));
      return {
        label: d.date.slice(5), // MM-DD
        fullDate: d.date,
        downloadMb: rxMb,
        uploadMb: txMb,
        downloadBytes: d.rx_bytes,
        uploadBytes: d.tx_bytes,
        totalBytes: d.rx_bytes + d.tx_bytes,
      };
    });
  }, [currentIface?.daily]);

  // Monthly table data (12m)
  const monthlyData = useMemo(() => {
    const raw = currentIface?.monthly ?? [];
    return raw.map((m) => ({
      month: m.month,
      downloadBytes: m.rx_bytes,
      uploadBytes: m.tx_bytes,
      totalBytes: m.rx_bytes + m.tx_bytes,
    }));
  }, [currentIface?.monthly]);

  return (
    <motion.div
      className={PAGE_ROOT}
      variants={staggerContainer}
      initial="hidden"
      animate="visible"
    >
      {/* ── Page Header & Controls ─────────────────────────────────────── */}
      <motion.div variants={staggerItem}>
        <div className={PAGE_HEAD.ROOT}>
          <div className={PAGE_HEAD.TITLES}>
            <div className="flex items-center gap-2.5">
              <h1 className={PAGE_HEAD.TITLE}>Bandwidth Monitoring & vnStat</h1>
              <Badge variant="success" className="gap-1 font-mono text-[0.6875rem]">
                <ActivityIcon className="size-3 animate-pulse" />
                <span>Live 1 Hz</span>
              </Badge>
            </div>
            <p className={PAGE_HEAD.DESC}>
              Real-time traffic throughput and rolling vnStat usage metrics across
              modem interfaces.
            </p>
          </div>

          <div className={PAGE_HEAD.ACTIONS}>
            {/* Interface Selector */}
            <div className="flex items-center gap-2">
              <NetworkIcon className="size-4 text-on-surface-variant flex-none" />
              <Select
                value={selectedIface}
                onValueChange={(val) => val && setSelectedIface(val)}
              >
                <SelectTrigger
                  className={cn(
                    "h-9 min-w-[9.5rem] rounded-pill border-0 bg-surface-container px-3 text-xs font-semibold text-on-surface",
                    FOCUS_RING
                  )}
                >
                  <SelectValue placeholder="Select interface" />
                </SelectTrigger>
                <SelectContent className="rounded-xl border-0 bg-surface shadow-lg">
                  {interfaceNames.map((name) => (
                    <SelectItem
                      key={name}
                      value={name}
                      className="text-xs font-medium cursor-pointer"
                    >
                      {name}
                      {name === data?.default_interface ? " (default)" : ""}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* Refresh Action */}
            <Button
              type="button"
              variant="outline"
              size="sm"
              onClick={handleRefresh}
              disabled={isLoading}
              className={cn(
                PILL_ACTION,
                "border-0 bg-surface-container hover:bg-surface-container-high text-on-surface",
                FOCUS_RING
              )}
            >
              <RefreshCwIcon
                className={cn("size-3.5", isLoading && "animate-spin")}
              />
              <span>Refresh</span>
            </Button>

            {/* Reset Dialog Trigger */}
            <AlertDialog open={resetDialogOpen} onOpenChange={setResetDialogOpen}>
              <AlertDialogTrigger asChild>
                <Button
                  type="button"
                  variant="outline"
                  size="sm"
                  disabled={isResetting}
                  className={cn(
                    PILL_ACTION,
                    "border-0 bg-surface-container hover:bg-destructive/10 text-destructive",
                    FOCUS_RING
                  )}
                >
                  <RotateCcwIcon className="size-3.5" />
                  <span>Reset Counters</span>
                </Button>
              </AlertDialogTrigger>
              <AlertDialogContent className="rounded-card border-0 bg-surface">
                <AlertDialogHeader>
                  <AlertDialogTitle>Reset Bandwidth Counters?</AlertDialogTitle>
                  <AlertDialogDescription className="text-on-surface-variant text-sm leading-relaxed">
                    This will reset traffic metrics for{" "}
                    <strong className="text-on-surface font-semibold">
                      {selectedIface}
                    </strong>{" "}
                    back to zero. Real-time rate tracking will continue normally.
                  </AlertDialogDescription>
                </AlertDialogHeader>
                <AlertDialogFooter>
                  <AlertDialogCancel className="rounded-pill border-0 bg-surface-container">
                    Cancel
                  </AlertDialogCancel>
                  <AlertDialogAction
                    onClick={handleResetConfirm}
                    className="rounded-pill bg-destructive text-destructive-foreground hover:bg-destructive/90"
                  >
                    Reset Now
                  </AlertDialogAction>
                </AlertDialogFooter>
              </AlertDialogContent>
            </AlertDialog>
          </div>
        </div>
      </motion.div>

      {/* ── Section 1: Live Speedometer & 60-Second Realtime Chart ────────── */}
      <motion.div variants={staggerItem} className="grid grid-cols-1 gap-5 lg:grid-cols-12">
        {/* Live Gauges (4 cols on lg) */}
        <div className="flex flex-col gap-3.5 lg:col-span-4">
          {/* Download Speed Card */}
          <Card className={cn(CARD_SHELL, "relative overflow-hidden border border-downlink/15")}>
            <div className="flex items-center justify-between">
              <span className="flex items-center gap-1.5 text-xs font-semibold text-downlink">
                <ArrowDownIcon className="size-4" />
                <span>DOWNLOAD (RX)</span>
              </span>
              <span className="size-2 rounded-full bg-downlink animate-pulse" />
            </div>

            <div className="mt-2.5 flex items-baseline gap-2">
              <span className="font-mono text-3xl lg:text-4xl font-extrabold tracking-tight text-on-surface">
                {bpsToMbps(rxBps).toFixed(2)}
              </span>
              <span className="text-sm font-semibold text-downlink">Mbps</span>
            </div>

            <div className="mt-3 flex items-center justify-between border-t border-surface-container-high/60 pt-2 text-[0.6875rem] text-on-surface-variant">
              <span>Peak: {formatBps(peakRx)}</span>
              <span>Today: {formatBytes(todayRx)}</span>
            </div>
          </Card>

          {/* Upload Speed Card */}
          <Card className={cn(CARD_SHELL, "relative overflow-hidden border border-uplink/15")}>
            <div className="flex items-center justify-between">
              <span className="flex items-center gap-1.5 text-xs font-semibold text-uplink">
                <ArrowUpIcon className="size-4" />
                <span>UPLOAD (TX)</span>
              </span>
              <span className="size-2 rounded-full bg-uplink animate-pulse" />
            </div>

            <div className="mt-2.5 flex items-baseline gap-2">
              <span className="font-mono text-3xl lg:text-4xl font-extrabold tracking-tight text-on-surface">
                {bpsToMbps(txBps).toFixed(2)}
              </span>
              <span className="text-sm font-semibold text-uplink">Mbps</span>
            </div>

            <div className="mt-3 flex items-center justify-between border-t border-surface-container-high/60 pt-2 text-[0.6875rem] text-on-surface-variant">
              <span>Peak: {formatBps(peakTx)}</span>
              <span>Today: {formatBytes(todayTx)}</span>
            </div>
          </Card>
        </div>

        {/* 60s Live Area Chart (8 cols on lg) */}
        <Card className={cn(CARD_SHELL, "lg:col-span-8 flex flex-col justify-between")}>
          <CardHeader className={CARD_PAD}>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle className={CARD_TITLE}>60-Second Real-Time Throughput</CardTitle>
                <CardDescription className={CARD_DESC}>
                  Instantaneous ingress & egress rates sampled at 1 Hz on {selectedIface}.
                </CardDescription>
              </div>
              <div className="flex items-center gap-3 text-xs font-medium">
                <span className="flex items-center gap-1 text-downlink">
                  <span className="size-2 rounded-full bg-downlink" />
                  <span>Download</span>
                </span>
                <span className="flex items-center gap-1 text-uplink">
                  <span className="size-2 rounded-full bg-uplink" />
                  <span>Upload</span>
                </span>
              </div>
            </div>
          </CardHeader>

          <CardContent className="px-0 pt-4 pb-1">
            <div className="h-[180px] w-full">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart
                  data={realtimeChartData}
                  margin={{ top: 8, right: 10, left: -15, bottom: 0 }}
                >
                  <defs>
                    <linearGradient id="downlinkGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-downlink)" stopOpacity={0.35} />
                      <stop offset="95%" stopColor="var(--color-downlink)" stopOpacity={0.0} />
                    </linearGradient>
                    <linearGradient id="uplinkGrad" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="5%" stopColor="var(--color-uplink)" stopOpacity={0.35} />
                      <stop offset="95%" stopColor="var(--color-uplink)" stopOpacity={0.0} />
                    </linearGradient>
                  </defs>
                  <CartesianGrid
                    strokeDasharray="3 3"
                    vertical={false}
                    stroke="var(--color-surface-container-high)"
                    opacity={0.6}
                  />
                  <XAxis
                    dataKey="time"
                    tickLine={false}
                    axisLine={false}
                    stroke="var(--color-on-surface-variant)"
                    fontSize={11}
                    interval={9}
                  />
                  <YAxis
                    tickLine={false}
                    axisLine={false}
                    stroke="var(--color-on-surface-variant)"
                    fontSize={11}
                    unit="M"
                  />
                  <Tooltip
                    content={({ active, payload }) => {
                      if (!active || !payload?.length) return null;
                      const rawRx = payload[0]?.payload?.rawRx ?? 0;
                      const rawTx = payload[0]?.payload?.rawTx ?? 0;
                      return (
                        <div className="rounded-xl border border-surface-container-high bg-surface px-3 py-2 shadow-md">
                          <div className="text-[0.6875rem] font-medium text-on-surface-variant">
                            {payload[0]?.payload?.time}
                          </div>
                          <div className="mt-1 flex flex-col gap-0.5 text-xs font-semibold">
                            <span className="text-downlink">
                              ↓ {formatBps(rawRx)} ({bpsToMbps(rawRx).toFixed(2)} Mbps)
                            </span>
                            <span className="text-uplink">
                              ↑ {formatBps(rawTx)} ({bpsToMbps(rawTx).toFixed(2)} Mbps)
                            </span>
                          </div>
                        </div>
                      );
                    }}
                  />
                  <Area
                    type="monotone"
                    dataKey="download"
                    stroke="var(--color-downlink)"
                    strokeWidth={2}
                    fillOpacity={1}
                    fill="url(#downlinkGrad)"
                    isAnimationActive={false}
                  />
                  <Area
                    type="monotone"
                    dataKey="upload"
                    stroke="var(--color-uplink)"
                    strokeWidth={2}
                    fillOpacity={1}
                    fill="url(#uplinkGrad)"
                    isAnimationActive={false}
                  />
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </CardContent>
        </Card>
      </motion.div>

      {/* ── Section 2: Usage Summary Cards ───────────────────────────────── */}
      <motion.div variants={staggerItem}>
        <div className={SUMMARY_GRID}>
          {/* Today */}
          <Card className={CARD_SHELL}>
            <div className="flex items-center justify-between text-on-surface-variant text-xs">
              <span className="font-semibold uppercase tracking-wider">Today (Hari Ini)</span>
              <ClockIcon className="size-4" />
            </div>
            <div className="mt-2">
              <div className="font-mono text-2xl font-bold tracking-tight text-on-surface">
                {formatBytes(todayTotal)}
              </div>
              <div className="mt-1.5 flex items-center gap-2 text-xs">
                <span className="text-downlink font-medium">↓ {formatBytes(todayRx)}</span>
                <span className="text-on-surface-variant/40">/</span>
                <span className="text-uplink font-medium">↑ {formatBytes(todayTx)}</span>
              </div>
            </div>
          </Card>

          {/* Yesterday */}
          <Card className={CARD_SHELL}>
            <div className="flex items-center justify-between text-on-surface-variant text-xs">
              <span className="font-semibold uppercase tracking-wider">Yesterday (Kemarin)</span>
              <CalendarIcon className="size-4" />
            </div>
            <div className="mt-2">
              <div className="font-mono text-2xl font-bold tracking-tight text-on-surface">
                {formatBytes(yesterdayData.total)}
              </div>
              <div className="mt-1.5 flex items-center gap-2 text-xs">
                <span className="text-downlink font-medium">↓ {formatBytes(yesterdayData.rx)}</span>
                <span className="text-on-surface-variant/40">/</span>
                <span className="text-uplink font-medium">↑ {formatBytes(yesterdayData.tx)}</span>
              </div>
            </div>
          </Card>

          {/* Last 7 Days */}
          <Card className={CARD_SHELL}>
            <div className="flex items-center justify-between text-on-surface-variant text-xs">
              <span className="font-semibold uppercase tracking-wider">Last 7 Days (7 Hari)</span>
              <TrendingUpIcon className="size-4" />
            </div>
            <div className="mt-2">
              <div className="font-mono text-2xl font-bold tracking-tight text-on-surface">
                {formatBytes(last7DaysData.total)}
              </div>
              <div className="mt-1.5 flex items-center justify-between text-xs">
                <div className="flex items-center gap-2">
                  <span className="text-downlink font-medium">↓ {formatBytes(last7DaysData.rx)}</span>
                  <span className="text-on-surface-variant/40">/</span>
                  <span className="text-uplink font-medium">↑ {formatBytes(last7DaysData.tx)}</span>
                </div>
                <span className="text-[0.6875rem] text-on-surface-variant">
                  Avg {formatBytes(last7DaysData.avg)}/d
                </span>
              </div>
            </div>
          </Card>

          {/* This Month */}
          <Card className={CARD_SHELL}>
            <div className="flex items-center justify-between text-on-surface-variant text-xs">
              <span className="font-semibold uppercase tracking-wider">This Month (Bulan Ini)</span>
              <HardDriveIcon className="size-4" />
            </div>
            <div className="mt-2">
              <div className="font-mono text-2xl font-bold tracking-tight text-on-surface">
                {formatBytes(thisMonthData.total)}
              </div>
              <div className="mt-1.5 flex items-center justify-between text-xs">
                <div className="flex items-center gap-2">
                  <span className="text-downlink font-medium">↓ {formatBytes(thisMonthData.rx)}</span>
                  <span className="text-on-surface-variant/40">/</span>
                  <span className="text-uplink font-medium">↑ {formatBytes(thisMonthData.tx)}</span>
                </div>
                <span className="text-[0.6875rem] text-on-surface-variant">
                  Total {formatBytes((currentIface?.total_rx_bytes ?? 0) + (currentIface?.total_tx_bytes ?? 0))}
                </span>
              </div>
            </div>
          </Card>
        </div>
      </motion.div>

      {/* ── Section 3: Tabbed Historical Breakdown ───────────────────────── */}
      <motion.div variants={staggerItem}>
        <Card className={CARD_SHELL}>
          <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
            <div className="flex flex-col gap-3 @2xl/card:flex-row @2xl/card:items-center @2xl/card:justify-between border-b border-surface-container-high/60 pb-3">
              <div>
                <CardTitle className={CARD_TITLE}>Historical Traffic Breakdown</CardTitle>
                <CardDescription className={CARD_DESC}>
                  Rolling aggregated statistics persisted by the QManager telemetry collector.
                </CardDescription>
              </div>

              <TabsList className="h-9 rounded-pill bg-surface-container p-1">
                <TabsTrigger
                  value="24h"
                  className="rounded-pill px-3 text-xs font-semibold data-[state=active]:bg-surface data-[state=active]:shadow-xs"
                >
                  Last 24 Hours
                </TabsTrigger>
                <TabsTrigger
                  value="30d"
                  className="rounded-pill px-3 text-xs font-semibold data-[state=active]:bg-surface data-[state=active]:shadow-xs"
                >
                  Last 30 Days
                </TabsTrigger>
                <TabsTrigger
                  value="monthly"
                  className="rounded-pill px-3 text-xs font-semibold data-[state=active]:bg-surface data-[state=active]:shadow-xs"
                >
                  12 Months
                </TabsTrigger>
              </TabsList>
            </div>

            {/* ── 24h Tab ──────────────────────────────────────────────── */}
            <TabsContent value="24h" className="mt-4 space-y-4">
              {hourlyData.length > 0 && (
                <div className="h-[200px] w-full pt-2">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart
                      data={hourlyData}
                      margin={{ top: 10, right: 10, left: -15, bottom: 0 }}
                    >
                      <CartesianGrid
                        strokeDasharray="3 3"
                        vertical={false}
                        stroke="var(--color-surface-container-high)"
                        opacity={0.6}
                      />
                      <XAxis
                        dataKey="label"
                        tickLine={false}
                        axisLine={false}
                        stroke="var(--color-on-surface-variant)"
                        fontSize={11}
                      />
                      <YAxis
                        tickLine={false}
                        axisLine={false}
                        stroke="var(--color-on-surface-variant)"
                        fontSize={11}
                        unit="MB"
                      />
                      <Tooltip
                        content={({ active, payload }) => {
                          if (!active || !payload?.length) return null;
                          const p = payload[0]?.payload;
                          return (
                            <div className="rounded-xl border border-surface-container-high bg-surface px-3 py-2 shadow-md">
                              <div className="text-[0.6875rem] font-medium text-on-surface-variant">
                                {p.fullHour}
                              </div>
                              <div className="mt-1 flex flex-col gap-0.5 text-xs font-semibold">
                                <span className="text-downlink">
                                  ↓ {formatBytes(p.downloadBytes)}
                                </span>
                                <span className="text-uplink">
                                  ↑ {formatBytes(p.uploadBytes)}
                                </span>
                                <span className="text-on-surface border-t border-surface-container-high/60 pt-0.5">
                                  Total: {formatBytes(p.totalBytes)}
                                </span>
                              </div>
                            </div>
                          );
                        }}
                      />
                      <Bar dataKey="downloadMb" name="Download" fill="var(--color-downlink)" radius={[4, 4, 0, 0]} />
                      <Bar dataKey="uploadMb" name="Upload" fill="var(--color-uplink)" radius={[4, 4, 0, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              )}

              <div className="overflow-x-auto rounded-xl border border-surface-container-high">
                <Table>
                  <TableHeader className="bg-surface-container/60">
                    <TableRow className="border-surface-container-high hover:bg-transparent">
                      <TableHead className="text-xs font-semibold">Hour</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Download (RX)</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Upload (TX)</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Total Volume</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {hourlyData.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={4} className="h-20 text-center text-xs text-on-surface-variant">
                          No hourly records collected yet.
                        </TableCell>
                      </TableRow>
                    ) : (
                      [...hourlyData].reverse().map((h) => (
                        <TableRow key={h.fullHour} className="border-surface-container-high/60">
                          <TableCell className="font-mono text-xs font-medium text-on-surface">
                            {h.fullHour}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right text-downlink font-medium">
                            {formatBytes(h.downloadBytes)}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right text-uplink font-medium">
                            {formatBytes(h.uploadBytes)}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right font-bold text-on-surface">
                            {formatBytes(h.totalBytes)}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </TabsContent>

            {/* ── 30d Tab ──────────────────────────────────────────────── */}
            <TabsContent value="30d" className="mt-4 space-y-4">
              {dailyData.length > 0 && (
                <div className="h-[200px] w-full pt-2">
                  <ResponsiveContainer width="100%" height="100%">
                    <BarChart
                      data={dailyData}
                      margin={{ top: 10, right: 10, left: -15, bottom: 0 }}
                    >
                      <CartesianGrid
                        strokeDasharray="3 3"
                        vertical={false}
                        stroke="var(--color-surface-container-high)"
                        opacity={0.6}
                      />
                      <XAxis
                        dataKey="label"
                        tickLine={false}
                        axisLine={false}
                        stroke="var(--color-on-surface-variant)"
                        fontSize={11}
                      />
                      <YAxis
                        tickLine={false}
                        axisLine={false}
                        stroke="var(--color-on-surface-variant)"
                        fontSize={11}
                        unit="MB"
                      />
                      <Tooltip
                        content={({ active, payload }) => {
                          if (!active || !payload?.length) return null;
                          const p = payload[0]?.payload;
                          return (
                            <div className="rounded-xl border border-surface-container-high bg-surface px-3 py-2 shadow-md">
                              <div className="text-[0.6875rem] font-medium text-on-surface-variant">
                                {p.fullDate}
                              </div>
                              <div className="mt-1 flex flex-col gap-0.5 text-xs font-semibold">
                                <span className="text-downlink">
                                  ↓ {formatBytes(p.downloadBytes)}
                                </span>
                                <span className="text-uplink">
                                  ↑ {formatBytes(p.uploadBytes)}
                                </span>
                                <span className="text-on-surface border-t border-surface-container-high/60 pt-0.5">
                                  Total: {formatBytes(p.totalBytes)}
                                </span>
                              </div>
                            </div>
                          );
                        }}
                      />
                      <Bar dataKey="downloadMb" name="Download" fill="var(--color-downlink)" radius={[4, 4, 0, 0]} />
                      <Bar dataKey="uploadMb" name="Upload" fill="var(--color-uplink)" radius={[4, 4, 0, 0]} />
                    </BarChart>
                  </ResponsiveContainer>
                </div>
              )}

              <div className="overflow-x-auto rounded-xl border border-surface-container-high">
                <Table>
                  <TableHeader className="bg-surface-container/60">
                    <TableRow className="border-surface-container-high hover:bg-transparent">
                      <TableHead className="text-xs font-semibold">Date</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Download (RX)</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Upload (TX)</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Total Volume</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {dailyData.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={4} className="h-20 text-center text-xs text-on-surface-variant">
                          No daily records collected yet.
                        </TableCell>
                      </TableRow>
                    ) : (
                      [...dailyData].reverse().map((d) => (
                        <TableRow key={d.fullDate} className="border-surface-container-high/60">
                          <TableCell className="font-mono text-xs font-medium text-on-surface">
                            {d.fullDate}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right text-downlink font-medium">
                            {formatBytes(d.downloadBytes)}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right text-uplink font-medium">
                            {formatBytes(d.uploadBytes)}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right font-bold text-on-surface">
                            {formatBytes(d.totalBytes)}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </TabsContent>

            {/* ── Monthly Tab ──────────────────────────────────────────── */}
            <TabsContent value="monthly" className="mt-4">
              <div className="overflow-x-auto rounded-xl border border-surface-container-high">
                <Table>
                  <TableHeader className="bg-surface-container/60">
                    <TableRow className="border-surface-container-high hover:bg-transparent">
                      <TableHead className="text-xs font-semibold">Month</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Download (RX)</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Upload (TX)</TableHead>
                      <TableHead className="text-xs font-semibold text-right">Total Volume</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {monthlyData.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={4} className="h-20 text-center text-xs text-on-surface-variant">
                          No monthly records collected yet.
                        </TableCell>
                      </TableRow>
                    ) : (
                      [...monthlyData].reverse().map((m) => (
                        <TableRow key={m.month} className="border-surface-container-high/60">
                          <TableCell className="font-mono text-xs font-medium text-on-surface">
                            {m.month}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right text-downlink font-medium">
                            {formatBytes(m.downloadBytes)}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right text-uplink font-medium">
                            {formatBytes(m.uploadBytes)}
                          </TableCell>
                          <TableCell className="font-mono text-xs text-right font-bold text-on-surface">
                            {formatBytes(m.totalBytes)}
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
            </TabsContent>
          </Tabs>
        </Card>
      </motion.div>
    </motion.div>
  );
}
