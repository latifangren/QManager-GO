import { AppLayout } from "@/components/app-layout";

export default function SystemSettingsLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <AppLayout>{children}</AppLayout>;
}
