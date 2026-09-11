export interface DnsPreset {
  id: string;
  name: string;
  tag: string;
  description: string;
  servers: string[];
}

export const DNS_PRESETS: DnsPreset[] = [
  {
    id: "cloudflare",
    name: "Cloudflare",
    tag: "1.1.1.1",
    description: "Ultra-fast, privacy-first DNS",
    servers: ["1.1.1.1", "1.0.0.1"],
  },
  {
    id: "cloudflare_security",
    name: "Cloudflare Security",
    tag: "Malware Block",
    description: "Blocks malware & malicious domains",
    servers: ["1.1.1.2", "1.0.0.2"],
  },
  {
    id: "cloudflare_family",
    name: "Cloudflare Family",
    tag: "Family Safe",
    description: "Blocks malware and adult content",
    servers: ["1.1.1.3", "1.0.0.3"],
  },
  {
    id: "google",
    name: "Google DNS",
    tag: "8.8.8.8",
    description: "Highly reliable global resolver",
    servers: ["8.8.8.8", "8.8.4.4"],
  },
  {
    id: "quad9",
    name: "Quad9",
    tag: "Security",
    description: "Blocks malicious domains with threat intelligence",
    servers: ["9.9.9.9", "149.112.112.112"],
  },
  {
    id: "adguard",
    name: "AdGuard DNS",
    tag: "Ad Block",
    description: "Blocks ads, trackers, and malicious sites",
    servers: ["94.140.14.14", "94.140.15.15"],
  },
  {
    id: "adguard_family",
    name: "AdGuard Family",
    tag: "Family Safe",
    description: "Blocks ads, trackers, and adult websites",
    servers: ["94.140.14.15", "94.140.15.16"],
  },
  {
    id: "opendns",
    name: "OpenDNS (Cisco)",
    tag: "208.67.222.222",
    description: "Enterprise-grade reliability and security",
    servers: ["208.67.222.222", "208.67.220.220"],
  },
  {
    id: "controld",
    name: "Control D",
    tag: "Unfiltered",
    description: "High-speed unfiltered privacy DNS",
    servers: ["76.76.2.0", "76.76.10.0"],
  },
];
