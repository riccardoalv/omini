# Testing Omini

Omini is young: so far it has run on a handful of networks. Thank you for
trying it on yours. This page explains how to try it safely, what to look at,
and how to tell us what went wrong. It takes about fifteen minutes to set up;
after that, just let it run for a few days and use it.

## What we need most

People with gear we have not seen much yet:

- **UniFi**, **TP-Link Omada**, **MikroTik**, **pfSense** or **OpenWrt**
- switches with SNMP from any brand, and access points in bridge mode
- **Proxmox** clusters, **AdGuard Home** or **Pi-hole**

A plain home network with one router and no integration is useful too: it
tests discovery and identification.

## Is it safe?

- **Omini only reads, unless you click.** It changes a device only when you
  click a change and confirm it (a port, a VLAN, an IP reservation, a DNS name,
  pausing a device's internet). Nothing changes on its own.
- **Use a read-only account** to be sure: [Integrations](integrations.md) shows
  how to create one for each integration. With it, every change fails before it
  touches anything.
- **Changes that cut Omini or the internet off are blocked**, unless you turn on
  **Settings → Changes → Allow destructive changes**. Leave it off.
- Omini scans your network (ARP, ping, common ports, mDNS, SSDP, SNMP with
  read-only communities). An IDS on your network may notice it.
- Plugins are Python scripts that run on your server with the credentials you
  give them. The official ones are open source.

## Setting up

1. Start Omini as in the [Quick start](../README.md#quick-start). It needs host
   networking to see your LAN.
2. Open `http://<your-server>:8080` and create the admin account.
3. Wait one or two rounds (a minute or two): the map fills in from the network
   scan alone.
4. Add your firewall, switches, access points or hypervisor in
   **Integrations → Add integration**, with a read-only account.

## What to look at

Use it as you normally would and note what surprises you. Some things worth
checking:

- **Discovery:** are devices missing? Is one device shown twice?
- **Identification:** is each device's type, brand and model right? The panel
  shows the evidence behind each guess.
- **The map:** is each device on the right switch port or access point? Are
  the links between your switches and the router right?
- **Traffic:** do the numbers match what your router shows?
- **Alerts:** did an alert fire that is not true, or not fire when something
  went wrong?
- **Restarts and time:** after a day or two, and after restarting the
  container, is everything still right? How much memory and CPU does it use?
- **The first impression:** what was confusing in the first ten minutes?

## Reporting

1. **Settings → About → Download diagnostics.** The file is what Omini
   collected and drew, anonymized: MACs keep only their vendor part; names,
   SSIDs, serial numbers and public addresses are replaced; private IPv4
   addresses stay; passwords, integration settings and configuration backups
   are left out. Look it over before sharing it.
2. Open an issue: [a bug](https://github.com/riccardoalv/omini/issues/new?template=bug_report.yml),
   [a device that is not recognized](https://github.com/riccardoalv/omini/issues/new?template=device_support.yml),
   or [a tester report](https://github.com/riccardoalv/omini/issues/new?template=tester_report.yml)
   when you just want to tell us how it went. Attach the diagnostics file.
3. For a plugin error, the log helps: `docker logs omini`, with
   `OMINI_LOG_LEVEL=debug` for more. Remove anything private first.

Ideas and questions go to [Discussions](https://github.com/riccardoalv/omini/discussions).
Blunt feedback is welcome: what is wrong, missing or not worth having.

## Stopping

[Uninstalling](installation.md#uninstalling) removes Omini. If you made
changes from Omini, read that section first: a few of them (a paused device, a
DNS name) stay on your router until you undo them.
