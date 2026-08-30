import { AppLayout } from "@/components/app-layout";

export default function SupportLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <AppLayout>{children}</AppLayout>;
}
