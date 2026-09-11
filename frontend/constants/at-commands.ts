/**
 * The functional groups the built-in presets fall into. The popover renders one
 * section per member, in this order.
 */
export const AT_COMMAND_CATEGORIES = [
  "modem",
  "network",
  "signal",
  "sim",
  "apn",
  "bands",
  "cell_lock",
  "passthrough",
] as const;

export type ATCommandCategory = (typeof AT_COMMAND_CATEGORIES)[number];

/** A user-defined preset. Its label is whatever the user typed, so it is not
 *  translatable and travels with the entry. */
export interface ATCommandPreset {
  label: string;
  command: string;
}

/** A built-in preset. Its label lives in the locale pack under
 *  `at_terminal.commands.presets.<id>`, so it is not carried here. */
export interface ATCommandDefault {
  id: string;
  command: string;
  category: ATCommandCategory;
}

/**
 * Default AT command presets tailored for Quectel 5G/LTE modems (RG501Q / RM520N).
 * Covers modem control, temperature, RF diagnostics, network mode, SIM/ICCID,
 * APN, carrier aggregation, cell locking, and IP passthrough.
 */
export const DEFAULT_AT_COMMANDS: ATCommandDefault[] = [
  // --- Modem & Hardware ---
  { id: "reboot", command: "AT+CFUN=1,1", category: "modem" },
  { id: "disconnect", command: "AT+CFUN=0", category: "modem" },
  { id: "connect", command: "AT+CFUN=1", category: "modem" },
  { id: "temp_info", command: "AT+QTEMP", category: "modem" },
  { id: "firmware_ver", command: "AT+QGMR", category: "modem" },
  { id: "imei_show", command: "AT+EGMR=0,7", category: "modem" },
  { id: "modem_info", command: "ATI", category: "modem" },

  // --- Network Mode & Registration ---
  { id: "mode_show", command: 'AT+QNWPREFCFG="mode_pref"', category: "network" },
  {
    id: "mode_auto",
    command: 'AT+QNWPREFCFG="mode_pref",AUTO',
    category: "network",
  },
  {
    id: "mode_nr_lte",
    command: 'AT+QNWPREFCFG="mode_pref",NR5G:LTE',
    category: "network",
  },
  {
    id: "mode_nr",
    command: 'AT+QNWPREFCFG="mode_pref",NR5G',
    category: "network",
  },
  {
    id: "mode_lte",
    command: 'AT+QNWPREFCFG="mode_pref",LTE',
    category: "network",
  },
  {
    id: "nr5g_disable_status",
    command: 'AT+QNWPREFCFG="nr5g_disable_mode"',
    category: "network",
  },
  {
    id: "nr5g_enable_both",
    command: 'AT+QNWPREFCFG="nr5g_disable_mode",0',
    category: "network",
  },
  {
    id: "nr5g_disable_sa",
    command: 'AT+QNWPREFCFG="nr5g_disable_mode",1',
    category: "network",
  },
  {
    id: "nr5g_disable_nsa",
    command: 'AT+QNWPREFCFG="nr5g_disable_mode",2',
    category: "network",
  },
  { id: "cops_query", command: "AT+COPS?", category: "network" },
  { id: "cereg_query", command: "AT+CEREG?", category: "network" },
  { id: "c5greg_query", command: "AT+C5GREG?", category: "network" },
  { id: "nwinfo_query", command: "AT+QNWINFO", category: "network" },

  // --- Signal & RF Quality ---
  { id: "signal_info", command: 'AT+QENG="servingcell"', category: "signal" },
  { id: "neighbour_info", command: 'AT+QENG="neighbourcell"', category: "signal" },
  { id: "ca_info", command: "AT+QCAINFO", category: "signal" },
  { id: "csq_query", command: "AT+CSQ", category: "signal" },

  // --- SIM Management ---
  { id: "sim_slot_get", command: "AT+QUIMSLOT?", category: "sim" },
  { id: "sim_slot_1", command: "AT+QUIMSLOT=1", category: "sim" },
  { id: "sim_slot_2", command: "AT+QUIMSLOT=2", category: "sim" },
  { id: "iccid_show", command: "AT+CCID", category: "sim" },
  { id: "cpin_query", command: "AT+CPIN?", category: "sim" },

  // --- APN & IP ---
  { id: "apn_list", command: "AT+CGDCONT?", category: "apn" },
  { id: "apn_ip_address", command: "AT+CGPADDR=1", category: "apn" },
  {
    id: "apn_nrbroadband",
    command: 'AT+CGDCONT=1,"IPV4V6","NRBROADBAND"',
    category: "apn",
  },

  // --- Band Locking / Queries ---
  {
    id: "bands_nr_sa",
    command: 'AT+QNWPREFCFG="nr5g_band"',
    category: "bands",
  },
  {
    id: "bands_nr_nsa",
    command: 'AT+QNWPREFCFG="nsa_nr5g_band"',
    category: "bands",
  },
  { id: "bands_lte", command: 'AT+QNWPREFCFG="lte_band"', category: "bands" },

  // --- Cell Lock Diagnostics & Reset ---
  { id: "cell_lock_lte_status", command: 'AT+QNWLOCK="common/4g"', category: "cell_lock" },
  { id: "cell_lock_lte_reset", command: 'AT+QNWLOCK="common/4g",0', category: "cell_lock" },
  { id: "cell_lock_5g_status", command: 'AT+QNWLOCK="common/5g"', category: "cell_lock" },
  { id: "cell_lock_5g_reset", command: 'AT+QNWLOCK="common/5g",0', category: "cell_lock" },

  // --- IP Passthrough & USB ---
  { id: "usbnet_mode", command: 'AT+QCFG="usbnet"', category: "passthrough" },
  {
    id: "ippt_addresses",
    command: 'AT+QMAP="WWAN"',
    category: "passthrough",
  },
  {
    id: "ippt_enable",
    command: 'AT+QMAP="MPDN_rule",0,1,0,1,1,"FF:FF:FF:FF:FF:FF"',
    category: "passthrough",
  },
  {
    id: "ippt_disable",
    command: 'AT+QMAP="MPDN_rule",0,1,0,0,1,"FF:FF:FF:FF:FF:FF"',
    category: "passthrough",
  },
];
