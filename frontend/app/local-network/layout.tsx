import { AppLayout } from "@/components/app-layout";

export default function LocalNetworkLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <AppLayout>{children}</AppLayout>;
}
