import { AppLayout } from "@/components/app-layout";

export default function CellularLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return <AppLayout>{children}</AppLayout>;
}
