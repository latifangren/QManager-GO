import { AppLayout } from "@/components/app-layout";

export default function MonitoringLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <AppLayout>{children}</AppLayout>;
}
