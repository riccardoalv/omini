/** Common services by protocol and port, to name a conversation's ports. */
const TCP: Record<number, string> = {
  20: 'FTP data',
  21: 'FTP',
  22: 'SSH',
  23: 'Telnet',
  25: 'SMTP',
  53: 'DNS',
  80: 'HTTP',
  110: 'POP3',
  139: 'NetBIOS',
  143: 'IMAP',
  443: 'HTTPS',
  445: 'SMB',
  465: 'SMTPS',
  548: 'AFP',
  587: 'SMTP',
  631: 'IPP',
  853: 'DNS over TLS',
  993: 'IMAPS',
  995: 'POP3S',
  1883: 'MQTT',
  2049: 'NFS',
  3000: 'Grafana',
  3306: 'MySQL',
  3389: 'RDP',
  5000: 'Synology',
  5001: 'Synology',
  5432: 'PostgreSQL',
  5900: 'VNC',
  6443: 'Kubernetes',
  8006: 'Proxmox',
  8080: 'HTTP',
  8096: 'Jellyfin',
  8123: 'Home Assistant',
  8443: 'HTTPS',
  8883: 'MQTTS',
  9000: 'Portainer',
  32400: 'Plex',
}
const UDP: Record<number, string> = {
  53: 'DNS',
  67: 'DHCP',
  68: 'DHCP',
  123: 'NTP',
  137: 'NetBIOS',
  161: 'SNMP',
  443: 'QUIC',
  500: 'IPsec',
  514: 'Syslog',
  1194: 'OpenVPN',
  1900: 'SSDP',
  3478: 'STUN',
  4500: 'IPsec',
  5353: 'mDNS',
  51820: 'WireGuard',
}
const PROTO: Record<number, string> = {
  1: 'ICMP',
  6: 'TCP',
  17: 'UDP',
  47: 'GRE',
  50: 'ESP',
  58: 'ICMPv6',
}

/** "HTTPS", "DNS", "TCP 8443"... */
export function serviceName(proto: number, port: number): string {
  const known = proto === 6 ? TCP[port] : proto === 17 ? UDP[port] : undefined
  if (known) return known
  const p = PROTO[proto] ?? `IP ${proto}`
  return port ? `${p} ${port}` : p
}
