"use client";

import type * as React from "react";
import { BugIcon, ExternalLinkIcon, FileTextIcon, GitForkIcon } from "lucide-react";
import { motion } from "motion/react";
import { useTranslation } from "react-i18next";

import { Button } from "@/components/ui/button";
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from "@/components/ui/card";
import { APP_VERSION } from "@/lib/i18n/app-version";
import { staggerContainer, staggerItem, staggerRowItem, staggerRows } from "@/lib/motion";
import { cn } from "@/lib/utils";

import {
  ACTION_LIST,
  ACTION_ROW,
  CARD_BODY,
  CARD_CELL,
  CARD_DESC,
  CARD_GRID,
  CARD_PAD,
  CARD_SHELL,
  CARD_TITLE,
  COMMUNITY,
  FOCUS_RING,
  PAGE_HEAD,
  PAGE_ROOT,
} from "./shapes";

const K = "support";

const GITHUB_REPO = "latifangren/QManager-GO";
const UPSTREAM_REPO = "dr-dolomite/QManager-RM520N";

const LINKS = {
  ISSUES: `https://github.com/${GITHUB_REPO}/issues`,
  ISSUES_LABEL: `github.com/${GITHUB_REPO}/issues`,
  RELEASES: `https://github.com/${GITHUB_REPO}/releases`,
  UPSTREAM: `https://github.com/${UPSTREAM_REPO}`,
  UPSTREAM_LABEL: `github.com/${UPSTREAM_REPO}`,
  DISCORD: "https://discord.iamromulan.dev/",
  DISCORD_NAME: "Cellular Modem Talk/Development",
} as const;

function DiscordIcon({ className }: { className?: string }): React.JSX.Element {
  return (
    <svg
      viewBox="0 0 24 24"
      fill="currentColor"
      className={className}
      aria-hidden="true"
    >
      <path d="M20.317 4.37a19.791 19.791 0 0 0-4.885-1.515.074.074 0 0 0-.079.037c-.21.375-.444.864-.608 1.25a18.27 18.27 0 0 0-5.487 0 12.64 12.64 0 0 0-.617-1.25.077.077 0 0 0-.079-.037A19.736 19.736 0 0 0 3.677 4.37a.07.07 0 0 0-.032.027C.533 9.046-.32 13.58.099 18.057a.082.082 0 0 0 .031.057 19.9 19.9 0 0 0 5.993 3.03.078.078 0 0 0 .084-.028c.462-.63.874-1.295 1.226-1.994a.076.076 0 0 0-.041-.106 13.107 13.107 0 0 1-1.872-.892.077.077 0 0 1-.008-.128 10.2 10.2 0 0 0 .372-.292.074.074 0 0 1 .077-.01c3.928 1.793 8.18 1.793 12.062 0a.074.074 0 0 1 .078.01c.12.098.246.198.373.292a.077.077 0 0 1-.006.127 12.299 12.299 0 0 1-1.873.892.077.077 0 0 0-.041.107c.36.698.772 1.362 1.225 1.993a.076.076 0 0 0 .084.028 19.839 19.839 0 0 0 6.002-3.03.077.077 0 0 0 .032-.054c.5-5.177-.838-9.674-3.549-13.66a.061.061 0 0 0-.031-.03zM8.02 15.33c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.956-2.419 2.157-2.419 1.21 0 2.176 1.085 2.176 2.419 0 1.333-.966 2.419-2.176 2.419zm7.974 0c-1.183 0-2.157-1.085-2.157-2.419 0-1.333.955-2.419 2.157-2.419 1.21 0 2.176 1.085 2.176 2.419 0 1.333-.966 2.419-2.176 2.419z" />
    </svg>
  );
}

interface ActionRowProps {
  href: string;
  icon: React.ReactNode;
  label: string;
  dest?: string;
  mono?: boolean;
  external?: boolean;
  newTabLabel: string;
}

function ActionRow({
  href,
  icon,
  label,
  dest,
  mono = false,
  external = true,
  newTabLabel,
}: ActionRowProps): React.JSX.Element {
  return (
    <motion.a
      variants={staggerRowItem}
      href={href}
      target={external ? "_blank" : undefined}
      rel={external ? "noopener noreferrer" : undefined}
      className={cn(ACTION_ROW.ROOT, FOCUS_RING)}
    >
      <span className={ACTION_ROW.DISC} aria-hidden="true">
        {icon}
      </span>
      <span className={ACTION_ROW.TEXT}>
        <span className={ACTION_ROW.LABEL}>{label}</span>
        {dest ? (
          <span className={cn(ACTION_ROW.DEST, mono && ACTION_ROW.DEST_MONO)}>
            {dest}
          </span>
        ) : null}
      </span>
      {external ? (
        <>
          <ExternalLinkIcon
            className={ACTION_ROW.EXT}
            aria-hidden="true"
          />
          <span className="sr-only">({newTabLabel})</span>
        </>
      ) : null}
    </motion.a>
  );
}

/**
 * `/support` — where to get help and find the community.
 */
export function SupportComponent(): React.JSX.Element {
  const { t } = useTranslation("common");
  const newTab = t(`${K}.opens_new_tab`);

  return (
    <motion.div
      className={PAGE_ROOT}
      initial="hidden"
      animate="visible"
      variants={staggerContainer}
    >
      <motion.header variants={staggerItem} className={PAGE_HEAD.ROOT}>
        <div className={PAGE_HEAD.TITLES}>
          <h1 className={PAGE_HEAD.TITLE}>{t(`${K}.page.title`)}</h1>
          <p className={PAGE_HEAD.DESC}>{t(`${K}.page.description`)}</p>
        </div>
      </motion.header>

      <div className={CARD_GRID}>
        <motion.div variants={staggerItem} className={CARD_CELL}>
          <Card className={CARD_SHELL}>
            <CardHeader className={CARD_PAD}>
              <CardTitle className={CARD_TITLE}>
                {t(`${K}.help.title`)}
              </CardTitle>
              <CardDescription className={CARD_DESC}>
                {t(`${K}.help.description`)}
              </CardDescription>
            </CardHeader>
            <CardContent className={cn(CARD_PAD, CARD_BODY)}>
              <motion.div variants={staggerRows} className={ACTION_LIST}>
                <ActionRow
                  href={LINKS.ISSUES}
                  icon={<BugIcon className={ACTION_ROW.GLYPH} />}
                  label={t(`${K}.help.bug.label`)}
                  dest={LINKS.ISSUES_LABEL}
                  mono
                  newTabLabel={newTab}
                />
                <ActionRow
                  href={LINKS.RELEASES}
                  icon={<FileTextIcon className={ACTION_ROW.GLYPH} />}
                  label={t(`${K}.help.release_notes.label`)}
                  dest={t(`${K}.help.release_notes.dest`, {
                    version: APP_VERSION,
                  })}
                  newTabLabel={newTab}
                />
                <ActionRow
                  href={LINKS.UPSTREAM}
                  icon={<GitForkIcon className={ACTION_ROW.GLYPH} />}
                  label={t(`${K}.help.upstream.label`)}
                  dest={LINKS.UPSTREAM_LABEL}
                  mono
                  newTabLabel={newTab}
                />
              </motion.div>
            </CardContent>
          </Card>
        </motion.div>

        <motion.div variants={staggerItem} className={CARD_CELL}>
          <Card className={CARD_SHELL}>
            <CardHeader className={CARD_PAD}>
              <CardTitle className={CARD_TITLE}>
                {t(`${K}.community.title`)}
              </CardTitle>
              <CardDescription className={CARD_DESC}>
                {t(`${K}.community.description`)}
              </CardDescription>
            </CardHeader>
            <CardContent className={cn(CARD_PAD, CARD_BODY)}>
              <div className={COMMUNITY.BODY}>
                <p className={COMMUNITY.COPY}>{t(`${K}.community.body`)}</p>
                <Button asChild variant="tonal" className="w-full">
                  <a
                    href={LINKS.DISCORD}
                    target="_blank"
                    rel="noopener noreferrer"
                    className="flex items-center justify-center gap-2"
                  >
                    <DiscordIcon className="size-4" />
                    <span>{t(`${K}.community.open_invite`)}</span>
                    <ExternalLinkIcon className="size-3.5" aria-hidden="true" />
                  </a>
                </Button>
              </div>
            </CardContent>
          </Card>
        </motion.div>
      </div>
    </motion.div>
  );
}

export default SupportComponent;
