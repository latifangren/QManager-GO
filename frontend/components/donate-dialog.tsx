"use client";

import { Button } from "@/components/ui/button";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";

// =============================================================================
// DonateDialog — Donation links triggered from sidebar
// =============================================================================

// Brand icons — not available in lucide-react

const WiseIcon = ({ className }: { className?: string }) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
    <path d="M13.553 0 9.3 13.401 6.235 4.136H0l5.696 15.728h7.209L24 0h-10.447z" />
    <path d="m13.856 19.864 2.974-8.216-3.558-2.302-4.206 10.518h4.79z" />
  </svg>
);

const PayPalIcon = ({ className }: { className?: string }) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
    <path d="M7.076 21.337H2.47a.641.641 0 0 1-.633-.74L4.944.901C5.026.382 5.474 0 5.998 0h7.46c2.57 0 4.578.543 5.69 1.81 1.01 1.15 1.304 2.42 1.012 4.287-.023.143-.047.288-.077.437-.983 5.05-4.349 6.797-8.647 6.797h-2.19c-.524 0-.968.382-1.05.9l-1.12 7.106zm14.146-14.42a3.35 3.35 0 0 0-.607-.541c-.013.076-.026.175-.041.254-.93 4.778-4.005 7.201-9.138 7.201h-2.19a.563.563 0 0 0-.556.479l-1.187 7.527h-.506l-.24 1.516a.56.56 0 0 0 .554.647h3.882c.46 0 .85-.334.922-.788.06-.26.76-4.852.816-5.09a.932.932 0 0 1 .923-.788h.58c3.76 0 6.705-1.528 7.565-5.946.36-1.847.174-3.388-.777-4.471z" />
  </svg>
);

const KofiIcon = ({ className }: { className?: string }) => (
  <svg viewBox="0 0 24 24" fill="currentColor" className={className} aria-hidden="true">
    <path d="M23.881 8.948c-.773-4.085-4.859-4.593-4.859-4.593H.723c-.604 0-.679.798-.679.798s-.082 7.324-.022 11.822c.164 2.424 2.586 2.672 2.586 2.672s8.267-.023 11.966-.049c2.438-.426 2.683-2.566 2.658-3.734 4.352.24 7.422-2.831 6.649-6.916zm-11.062 3.511c-1.246 1.453-4.011 3.976-4.011 3.976s-.121.119-.31.023c-.076-.057-.108-.09-.108-.09-.443-.441-3.368-3.049-4.034-3.954-.709-.965-1.041-2.7-.091-3.71.951-1.01 3.005-1.086 4.363.407 0 0 1.565-1.782 3.468-.963 1.904.82 1.832 3.011.723 4.311zm6.173.478c-.928.116-1.682.028-1.682.028V7.284h1.77s1.971.551 1.971 2.638c0 1.913-.985 2.667-2.059 3.015z" />
  </svg>
);

interface DonateDialogProps {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}

const DonateDialog = ({ open, onOpenChange }: DonateDialogProps) => {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-sm md:max-w-lg">
        <DialogHeader>
          <DialogTitle className="text-xl">
            Give a Tip to QManager
          </DialogTitle>
          <DialogDescription>
            Support the development of this project.
          </DialogDescription>
        </DialogHeader>
        <div className="grid gap-2 text-sm text-pretty font-medium leading-relaxed">
          <p>Hi, I&apos;m Rus 👋</p>
          <p>
            QuecManager is a little side project I maintain for free as part
            of Cameron&apos;s Toolkit. If you&apos;ve found it useful,
            consider supporting it with a small donation &mdash; it means a
            lot and keeps me going.
          </p>
          <p>Thanks so much for being awesome! 💙</p>
          <p className="text-xs text-muted-foreground">
            You can also tip via{" "}
            <a
              href="https://github.com/sponsors/dr-dolomite"
              target="_blank"
              rel="noopener noreferrer"
              className="underline underline-offset-2 hover:text-foreground transition-colors"
            >
              GitHub Sponsors
            </a>
            .
          </p>
        </div>
        <DialogFooter className="flex flex-row items-start gap-2 sm:justify-start">
          <Button
            asChild
            size="sm"
            className="bg-[#163300] hover:bg-[#1f4a00] text-white"
          >
            <a
              href="https://wise.com/pay/business/blackcatdev?currency=USD"
              target="_blank"
              rel="noopener noreferrer"
            >
              <WiseIcon className="size-4" />
              Wise
            </a>
          </Button>
          <Button
            asChild
            size="sm"
            className="bg-[#003087] hover:bg-[#002070] text-white"
          >
            <a
              href="https://paypal.me/iamrusss"
              target="_blank"
              rel="noopener noreferrer"
            >
              <PayPalIcon className="size-4" />
              PayPal
            </a>
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
};

export default DonateDialog;
