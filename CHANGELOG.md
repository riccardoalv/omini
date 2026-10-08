# Changelog

## [1.0.0](https://github.com/riccardoalv/omini/compare/v0.3.0...v1.0.0) (2026-10-08)


### Features

* **collector:** collect in rounds, one integration at a time, edge to center ([f7f1373](https://github.com/riccardoalv/omini/commit/f7f137390f4126e69a48354dd548267acef04156))
* **collector:** remembered switch ports survive restarts ([7a47524](https://github.com/riccardoalv/omini/commit/7a4752493da9a65dfda738e5f4ee581c8efd530b))
* **flows:** who talks to whom, from NetFlow, IPFIX and sFlow ([daa1fcc](https://github.com/riccardoalv/omini/commit/daa1fcc9f54011f2eeaf4f65c303e3c4c75303d6))
* insights, traffic history, presence timeline and animated flow ([4975226](https://github.com/riccardoalv/omini/commit/4975226f13a0bec9ab0b0667757c2171be27e91e))
* **insights:** ten new alerts and popups of the open ones ([595f238](https://github.com/riccardoalv/omini/commit/595f23802280ba472cef013e76c96652ae88b081))
* **map:** a click on a Wi-Fi network folds or unfolds its clients ([cd193ca](https://github.com/riccardoalv/omini/commit/cd193caee2b2640ff869cf34fa981082868ae4ba))
* **map:** a Wi-Fi client's panel shows its traffic even while idle ([456add8](https://github.com/riccardoalv/omini/commit/456add8416687b4d79052d09a5e064c172d66668))
* **map:** automatic areas per VLAN and subnet, any area color, no link animation ([0f07b20](https://github.com/riccardoalv/omini/commit/0f07b20bbd94864a791761f6cc80410232497a32))
* **map:** draw.io export, VLAN view, device extras and six new plugins in the store ([5323270](https://github.com/riccardoalv/omini/commit/5323270c791071b58fadaeeae74d282bdf8fbb07))
* **map:** every node can fold its children ([02df595](https://github.com/riccardoalv/omini/commit/02df595ff806c8eebaaa6782e2eefd9d75c1eb56))
* **map:** export dialog (format, theme, orientation); layout keeps grids and areas in order ([d0587cc](https://github.com/riccardoalv/omini/commit/d0587cc4bd582c4675701cda721861936a469c98))
* **map:** export the map as PNG, SVG or JSON ([0654719](https://github.com/riccardoalv/omini/commit/0654719edd31d768c0e9ae504e5ba80583d76a53))
* **map:** exported images show every group and area expanded ([dd9305e](https://github.com/riccardoalv/omini/commit/dd9305ef06cdf6e96f08404fa921061169255d15))
* **map:** fold a host's VMs like clients ([5c5db4c](https://github.com/riccardoalv/omini/commit/5c5db4cef7c02ba106720066f15725bef23350d8))
* **map:** frame what the VLAN filter highlights; two alerts as popups ([0c6feb1](https://github.com/riccardoalv/omini/commit/0c6feb14bf44b27af9add555e4b681a88015ed2d))
* **map:** nicer area color picker; hide empty scanned networks ([5135530](https://github.com/riccardoalv/omini/commit/513553018dfb02214136488416262600c5579d9c))
* **map:** siblings grouped by the port or Wi-Fi network they use ([3b8d8b8](https://github.com/riccardoalv/omini/commit/3b8d8b8e40bbc5bff7ef31cd665949daea0b7179))
* **map:** strict areas; VLANs and subnets as a filter ([0d3ef1a](https://github.com/riccardoalv/omini/commit/0d3ef1ac3fae838ef194e0d61a293e819106f7fd))
* **map:** tidy tree layout, pills clear of badges and areas, hide areas ([ad3c532](https://github.com/riccardoalv/omini/commit/ad3c5326036a1d9e87eca0746d0e240e4d26780e))
* **map:** Wi-Fi clients show their network and band on themselves ([5e07209](https://github.com/riccardoalv/omini/commit/5e07209d0f4cedfb73b0ce9f846717930c4777ce))
* **map:** Wi-Fi clients show their traffic, band and link rate ([810ee3b](https://github.com/riccardoalv/omini/commit/810ee3b32c10bb6c77849dd6c04d4039958ed626))
* **map:** Wi-Fi networks as mini nodes between an access point and its clients ([3838b7d](https://github.com/riccardoalv/omini/commit/3838b7df87831c71e4578e1681ffa2d8c58631d4))
* **netscan:** also scan the networks the routers report ([dd27a14](https://github.com/riccardoalv/omini/commit/dd27a14c09142d8fd4dc93dba5bb65250a0e2967))
* **netscan:** check what discovery can do here and say how to fix it ([f3a43d3](https://github.com/riccardoalv/omini/commit/f3a43d345c365dedcb997b176fdc6c5683e9ecd6))
* **notify:** notifications grouped by device, as cards in chat services ([89dc12a](https://github.com/riccardoalv/omini/commit/89dc12a1be3902f8f3726f5526701a1193742754))
* **notify:** send alerts to a webhook, Telegram or e-mail ([10c9188](https://github.com/riccardoalv/omini/commit/10c91880fadeda8b5ae119933ce3208017b7ce54))
* **plugins:** remote store index, store screen and the review process ([6942004](https://github.com/riccardoalv/omini/commit/6942004a4d32055d0ed0906bd796d0022db614a9))
* **schema:** number of CPUs and memory sizes in the device panel ([d15f7f9](https://github.com/riccardoalv/omini/commit/d15f7f9e3dc24afab76812a99db30c0b357ac62f))
* **schema:** VLANs, transceiver diagnostics, services, VPN peers, DHCP pools, firewall states ([2c53a80](https://github.com/riccardoalv/omini/commit/2c53a80d683d8b1040592311f2dd6faf080948f6))
* **snmp:** SNMP v3 and YAML vendor profiles ([2ce42d0](https://github.com/riccardoalv/omini/commit/2ce42d04b0eb57aa8d1fdc63091f24f5bdf95a39))
* **topology:** find an unmanaged switch beside a device that lists its clients ([eebd715](https://github.com/riccardoalv/omini/commit/eebd715634b84ef6e7f45cfd0a1f53a0be0adff4))
* **ui:** integration logos in the list and the store; a sharp Mercusys logo ([0981268](https://github.com/riccardoalv/omini/commit/0981268a07d98ee0688bf6d428536503af7aa163))
* **web:** Flows in the menu only while on; the store back to a modal ([5e8aa73](https://github.com/riccardoalv/omini/commit/5e8aa733fd70227c9699eb182e3a22d7ab919144))
* **web:** rename the Insights screen to Alerts ([2248672](https://github.com/riccardoalv/omini/commit/2248672bb5156948d15ccbd5e9b0a754817d4290))
* **web:** settings with a section menu and notifications as accordions ([805b48a](https://github.com/riccardoalv/omini/commit/805b48a0ab5929a587ac0de0957513a35d275620))
* **web:** the device list is a drawer on the map ([e8524d2](https://github.com/riccardoalv/omini/commit/e8524d22021558adcaadd08680948520321d9c06))


### Bug Fixes

* **classify:** TVs, speakers, IP phones, UPSes and stricter app titles ([f55d232](https://github.com/riccardoalv/omini/commit/f55d23285e98b972f20d7e1ad9ed7c7508951aa0))
* **collector:** a rate above what the port carries is a glitch, not traffic ([4ec3e55](https://github.com/riccardoalv/omini/commit/4ec3e55b1e81dcdb6182b46d57d6576a7c9a5811))
* **collector:** presence and flow listeners follow the round interval ([6ef1126](https://github.com/riccardoalv/omini/commit/6ef112643949db88a38accc3a370a60c00ecf759))
* **collector:** the device list says where devices are connected ([608bfdd](https://github.com/riccardoalv/omini/commit/608bfdd95bdb485f5e37c8b1c291203bf50807e1))
* **map:** a client's traffic badge no longer covers its name ([fa71540](https://github.com/riccardoalv/omini/commit/fa715404f47f8b58b7fa9ce18f1354e9d50d3073))
* **map:** a second parent's pill no longer covers the tree link's ([761f81a](https://github.com/riccardoalv/omini/commit/761f81acecc212da0518babce01eb32b6026b50a))
* **map:** an area follows its members into a bubble ([a4f9d64](https://github.com/riccardoalv/omini/commit/a4f9d64a6a868f1a05f9c7240e8426918bff42c7))
* **map:** areas keep their devices together; no false new devices ([a7d4ee7](https://github.com/riccardoalv/omini/commit/a7d4ee7d2b834a06dc5405c1ff7e75e2770fb3d1))
* **map:** expanding a group no longer opens the side panel ([357feca](https://github.com/riccardoalv/omini/commit/357fecaa64212e14a1b528e800d19f1cb5f153dc))
* **map:** exported images frame the whole map instead of a shifted, cut one ([e8a4d83](https://github.com/riccardoalv/omini/commit/e8a4d830992eef554e1984a0b4e94321a3dd4f78))
* **map:** exported SVG draws the wires as lines, not black triangles ([f27608a](https://github.com/riccardoalv/omini/commit/f27608ae2fa5567b7447346394533637b788ab8a))
* **map:** one machine, one node; no group button in the panel ([8cd136e](https://github.com/riccardoalv/omini/commit/8cd136eb93ccf39cd0d4de712d541e4acf406ada))
* **map:** steady switch uplinks; access points collapse with their Wi-Fi networks ([b46aed4](https://github.com/riccardoalv/omini/commit/b46aed49411f8a11e5c216d5a0631c50c54b18bf))
* **map:** the layout keeps the siblings' order, so Wi-Fi groups never interleave ([7dd34a3](https://github.com/riccardoalv/omini/commit/7dd34a30d5e1a7ae78f4f0e103ee4550ebfc2fd2))
* **map:** the VLAN/subnet filter no longer lights the LAN under the firewall ([be1ea6c](https://github.com/riccardoalv/omini/commit/be1ea6c88098be19c27b2dc7c28d0b4f28ea9d2a))
* **map:** traffic badges on top of every node, compact on clients ([9dc7011](https://github.com/riccardoalv/omini/commit/9dc7011cb1a02b3392556c8ea3c702b505bacc6c))
* **map:** Wi-Fi network nodes can be dragged ([b2ac45a](https://github.com/riccardoalv/omini/commit/b2ac45aa111e45b49b241e6ab703d7373b517cb9))
* **map:** Wi-Fi network nodes wide enough for their names ([2befabb](https://github.com/riccardoalv/omini/commit/2befabb7ca4b07f23acfe7158a81bf090594fd0d))
* **topology:** a device without MACs is the machine its address points to ([75267ca](https://github.com/riccardoalv/omini/commit/75267ca9cba7a8406a09b978d9545177cc4c69a2))
* **topology:** a MAC only a switch table has, behind an AP that does not list it, is not shown ([3c57867](https://github.com/riccardoalv/omini/commit/3c5786774f9955a96e8f9a54c43035b43a6e37ff))
* **topology:** guests reported by an integration hang under their host, which stays on its port ([649ac54](https://github.com/riccardoalv/omini/commit/649ac54f7d29c21d79c51b143c7a6db84698ca39))
* **topology:** integration devices never float apart; brand icon for unknown types ([28c925c](https://github.com/riccardoalv/omini/commit/28c925c6a2e25eecb6f9e41574bec86449d0df25))
* **topology:** nested routers, firewall VMs seen by their host, quieter alerts ([09fb05e](https://github.com/riccardoalv/omini/commit/09fb05e262ded7dcbc38f3656a59258696f1be3a))
* **topology:** port-channels, site-to-site VPNs, routed scans, LLDP phones ([5730746](https://github.com/riccardoalv/omini/commit/5730746ea9078a63100201a7ece1cf98e66734ec))
* **web:** popups clear of the device panel; updates without a version ([0d40547](https://github.com/riccardoalv/omini/commit/0d405475fe9927d6773ab1026af19950a464e94b))


### Documentation

* banner, screenshots and a README for the first official release ([8e80c5b](https://github.com/riccardoalv/omini/commit/8e80c5beb991ddec66fb21c74b0f766810580a75))

## [0.3.0](https://github.com/riccardoalv/omini/compare/v0.2.0...v0.3.0) (2026-10-07)


### Features

* Horaco and Mercusys in the store; clients keep their switch port while the switch forgets them ([f854e60](https://github.com/riccardoalv/omini/commit/f854e609751c423029a95a9cef376a58c32a73fb))
* **map:** port options on click, port names mid-link, switch uplinks from MAC tables ([1421e7b](https://github.com/riccardoalv/omini/commit/1421e7b2f40b809a55a386e5c848ad51b15c7453))

## [0.2.0](https://github.com/riccardoalv/omini/compare/v0.1.2...v0.2.0) (2026-10-07)


### Features

* **map:** resizable device panel with a clearer layout; quick nmap versions by default ([937c5e5](https://github.com/riccardoalv/omini/commit/937c5e5d693d0356866723d91e0d9a287b3c8c45))
* **map:** show the port's name next to the link speed ([50ebe30](https://github.com/riccardoalv/omini/commit/50ebe306a201910c3294b0b62a8f57c527721957))
* **nmap:** configurable device scan, light by default ([9fd084e](https://github.com/riccardoalv/omini/commit/9fd084ef089676107f73116c05fdfe6cb94e47ac))
* **nmap:** deep scan from the device panel (top 1024 ports, -sV -sC, OS with permission) ([6f997b2](https://github.com/riccardoalv/omini/commit/6f997b2b853c2343990f193d109d56006436d273))
* scan one device with nmap from its panel ([ba2a447](https://github.com/riccardoalv/omini/commit/ba2a4478610922b7d0a67bc503743d6311dfd0cd))
* **schema:** system health — load, swap, disks, temperatures and pending updates ([3da871c](https://github.com/riccardoalv/omini/commit/3da871c05a13587bf206d2a8334b86e8a2418dd8))

## [0.1.2](https://github.com/riccardoalv/omini/compare/v0.1.1...v0.1.2) (2026-10-07)


### Bug Fixes

* nmap no longer adds a second gateway; traffic shown on devices ([036da53](https://github.com/riccardoalv/omini/commit/036da53cf27cc01353356d51452eceaae6f0fc29))

## [0.1.1](https://github.com/riccardoalv/omini/compare/v0.1.0...v0.1.1) (2026-10-07)


### Bug Fixes

* **map:** the map screen was blank since areas could be collapsed ([d8f41b1](https://github.com/riccardoalv/omini/commit/d8f41b19e8efd3e4b4e884fd62a66da56df54586))

## 0.1.0 (2026-10-07)


### ⚠ BREAKING CHANGES

* the "snmp" integration type and POST /api/discovery/scan no longer exist.

### Features

* a single network scan; "Add integration" opens the store ([bfe7a30](https://github.com/riccardoalv/omini/commit/bfe7a3005a5073a7f4fca8331465155fe7dfa2e3))
* add the omini server command ([c88d056](https://github.com/riccardoalv/omini/commit/c88d056ab817c6ba6f81be8b5fb4bb0a37f3efcf))
* **api:** add HTTP API and serve the embedded web UI ([7229ef3](https://github.com/riccardoalv/omini/commit/7229ef3a91e13f0757ee174f984ad4846f0af3ce))
* **api:** detect devices' web interfaces ([6f54559](https://github.com/riccardoalv/omini/commit/6f545599515b7508df127b5bda07f12cd0214a86))
* **api:** include the device role in the inventory ([5919e2c](https://github.com/riccardoalv/omini/commit/5919e2cdbc908d72fd2ff9d4619ca92d698139c1))
* **api:** run an integration now, skipping its caches ([62c9172](https://github.com/riccardoalv/omini/commit/62c9172ea166058f11c6869e775e8692741d1928))
* app nodes with catalog icons, broader port scan and a steady map ([a8abd74](https://github.com/riccardoalv/omini/commit/a8abd7446c5a261f18c5f0cd8eef0ab093f1bf7b))
* areas follow their devices; UI language saved per user ([531d8ea](https://github.com/riccardoalv/omini/commit/531d8ea96c43d30f1d0c035ed4a62fcb7aa2d2c0))
* **auth:** add first-run admin setup and session login ([87186c0](https://github.com/riccardoalv/omini/commit/87186c053a6a8b88211ee93ad23bfe3aa3684dfb))
* classify map nodes and let users correct the result ([45a279a](https://github.com/riccardoalv/omini/commit/45a279aa097f7682cf2f810ced8e343c04e4a18d))
* **classify:** identify device type, OS, brand and homelab software ([48d685a](https://github.com/riccardoalv/omini/commit/48d685a2d390617d987e753f3e460eb7bf9b3d96))
* **classify:** recognize air conditioners and count scanned hosts in status ([ecac310](https://github.com/riccardoalv/omini/commit/ecac3105e567ba82af63798789ae6ffdc689b567))
* **classify:** recognize solar inverters ([5969907](https://github.com/riccardoalv/omini/commit/5969907a012e98865fa569a22ed463e629c64237))
* collection interval per integration ([c102a50](https://github.com/riccardoalv/omini/commit/c102a5024cc6f1eeb5e7f7af98a525ddcb00816d))
* **collector:** place Proxmox VMs under the Proxmox host ([29f2981](https://github.com/riccardoalv/omini/commit/29f2981319476d024387156769210a9551143bd9))
* **collector:** poll integrations and maintain the map state ([84f474e](https://github.com/riccardoalv/omini/commit/84f474e8c6ff4bc78fabe9daf8c7f08ca851648e))
* **demo:** add a fictional homelab network ([c6b6c30](https://github.com/riccardoalv/omini/commit/c6b6c3016433a98d9472974e443a25e8ecc9cd53))
* Docker image, published with each release ([5bcd3f4](https://github.com/riccardoalv/omini/commit/5bcd3f47f715ac4a5083bee5fca4a1d82b1e8300))
* hide or delete a device from its panel ([73827d4](https://github.com/riccardoalv/omini/commit/73827d47991389c79af3aee648a2b801905fe9fe))
* **integration:** normalize configs and protect secret fields ([4789dd2](https://github.com/riccardoalv/omini/commit/4789dd2f3e74c51bac93d17048bdaccc8e8c22ce))
* live traffic on the map ([65ee2b0](https://github.com/riccardoalv/omini/commit/65ee2b07691cbf248dc9284b68175df7e53037c9))
* **map:** a node in an area brings its children ([2c5c53e](https://github.com/riccardoalv/omini/commit/2c5c53e281acb6a792f0ee2b5a87bdc62c1fb0d1))
* **map:** collapse an area into a bubble ([9b1f2b9](https://github.com/riccardoalv/omini/commit/9b1f2b93ee5c944349bcca9e1dc3ae3842de2301))
* **map:** compact top-down view with leaf children in grids ([44296a1](https://github.com/riccardoalv/omini/commit/44296a183b29c622d0a4cc7c26226a1e02af588d))
* **map:** lay out areas as boxes so other devices stay out ([91bac5f](https://github.com/riccardoalv/omini/commit/91bac5f24875c0ab53e6ecd63df342d297c72815))
* **map:** named areas to group devices; expand without overlaps ([e680118](https://github.com/riccardoalv/omini/commit/e6801183c9258e72694642e0400675f456583fce))
* **map:** port front view, CPU/memory bars and link speeds ([a598ba5](https://github.com/riccardoalv/omini/commit/a598ba527b6770fc96fb1132fb30fc4e505968e6))
* **netscan:** collect web titles, SSH banners, ping TTL and this server's OS ([7e1a7db](https://github.com/riccardoalv/omini/commit/7e1a7db486443e54572ad4f43368e1fdef73938e))
* **netscan:** find every device on the network with zero setup ([65d2774](https://github.com/riccardoalv/omini/commit/65d2774d2713d1d2f700ded0abd498e33eec462b))
* **netscan:** make every discovery method configurable ([6895744](https://github.com/riccardoalv/omini/commit/68957449bb943b0d44da37fe243ed068f571a155))
* nmap integration and device model names ([676f93f](https://github.com/riccardoalv/omini/commit/676f93faa4b82b26cb1d8393bc2d6e0089023767))
* **oui:** embed the IEEE MAC vendor database ([f661e2b](https://github.com/riccardoalv/omini/commit/f661e2bbd415384b389de75831f33f186256655a))
* **panel:** describe ports ([5e92d51](https://github.com/riccardoalv/omini/commit/5e92d51a65246061e815fefadeee2e738f8274f9))
* **plugins:** install any GitHub repository; curated catalog with OPNsense ([6558e78](https://github.com/riccardoalv/omini/commit/6558e7834fb4f75c44e07a0ae0bbcba577884601))
* **plugins:** Python plugin runtime, SDK and plugin management ([c739fa2](https://github.com/riccardoalv/omini/commit/c739fa2fb18a46b83a53bb223520dc8c15c94fd1))
* scan the network automatically on first start ([605f0ef](https://github.com/riccardoalv/omini/commit/605f0efed6aaeafe47c2d08bb2c9ec3008c11056))
* **schema:** add data contract and code generation ([00b7232](https://github.com/riccardoalv/omini/commit/00b7232d983a2b8f25a2c2677dabdb6fb3f5d2d0))
* **schema:** add hosts observed by integrations ([61d6061](https://github.com/riccardoalv/omini/commit/61d60611b5d12f3ea7ff43592787e61c875d7b3f))
* **schema:** group form fields into sections ([24a376b](https://github.com/riccardoalv/omini/commit/24a376bc59e5e5992641bf8243d7d6fc500a2873))
* **schema:** record web titles, banners and TTL of hosts ([c1ca073](https://github.com/riccardoalv/omini/commit/c1ca073ad503c0855d009c09bc773791ba89d7e6))
* **secret:** encrypt sensitive config values at rest ([45ba65c](https://github.com/riccardoalv/omini/commit/45ba65cb5761f16a8862ded7a76c1791519116ab))
* SNMP is a method of the network scan; integration names are fixed ([5509a8d](https://github.com/riccardoalv/omini/commit/5509a8dfe4eb3196658cce6e4ff8a169e0ade649))
* **snmp:** add generic SNMP v2c integration ([82acdbe](https://github.com/riccardoalv/omini/commit/82acdbe125b1ca0327550af93fec8d607df95b7d))
* **snmp:** discover SNMP devices in a subnet ([55649c7](https://github.com/riccardoalv/omini/commit/55649c7c6daa84bdaaf5353692cc3c8ba65a3035))
* **store:** add SQLite store with migrations ([6fb4e13](https://github.com/riccardoalv/omini/commit/6fb4e13bdb8a735343ee0bf4e3b02f9b491c5226))
* **topology:** build the network map from collected devices ([88c3dce](https://github.com/riccardoalv/omini/commit/88c3dce590ff75d2c8ab45c37b8b52c8537a76be))
* **topology:** place scanned hosts and keep every integration's data ([b8b8323](https://github.com/riccardoalv/omini/commit/b8b8323ec81c35fc4223e4bff71ac38110b8dd66))
* WAN nodes above the firewall; RJ45 and SFP ports ([ad8b6c2](https://github.com/riccardoalv/omini/commit/ad8b6c2a077791ca3b6fab30679958f799626a1a))
* **web:** add the web UI ([4472dc1](https://github.com/riccardoalv/omini/commit/4472dc1d2dddb008c9433239c7f55868d4c16fbf))
* **web:** device icons with brand logos and identification panel ([24d1222](https://github.com/riccardoalv/omini/commit/24d1222f0b0e4915c3bed675081543997b9da0a0))
* **web:** expandable integrations, device names, letter badges, context menu ([98082a3](https://github.com/riccardoalv/omini/commit/98082a3818bc294fbc2d528debb582a3aa6a9ea3))
* **web:** open device web interfaces, map orientation and sidebar ([9c7a806](https://github.com/riccardoalv/omini/commit/9c7a80680f7e473b903dd9a76bfcfbef59a9876e))
* **web:** plugin store modal with search and add by URL ([ba52d25](https://github.com/riccardoalv/omini/commit/ba52d25785bb6d61d62389711058b697524671e9))
* **web:** run now, hide offline devices, drop discovery dialog and letter badges ([1d74b5b](https://github.com/riccardoalv/omini/commit/1d74b5b200c53b35c02f939424fc103d6a1223c0))
* **web:** scan button on the empty map, switches for yes/no fields ([7fc67d2](https://github.com/riccardoalv/omini/commit/7fc67d283328da3dc77632dbdfde544442b6a34c))
* **web:** show/hide toggle for secrets; blank keeps the saved one ([bc26cfa](https://github.com/riccardoalv/omini/commit/bc26cfaa611ad19e60015bc93740de3f615af417))
* **web:** use a toggle switch to enable integrations ([fe75977](https://github.com/riccardoalv/omini/commit/fe75977a012f2a06e8de1f0c0cbb04c639144ed9))


### Bug Fixes

* **integrations:** accept addresses without https:// in URL fields ([9b2376a](https://github.com/riccardoalv/omini/commit/9b2376a07f8023628a47f989183975ece8d7774a))
* **map:** areas show in both orientations; hidden devices do not make the map look empty ([e637b1c](https://github.com/riccardoalv/omini/commit/e637b1c4152f30289f63025a2ea477d3b1bc804c))
* **map:** automatic nodes never land on dragged ones ([1cb7dbf](https://github.com/riccardoalv/omini/commit/1cb7dbf659b1adc92cf1e156b8dca16463950ee1))
* **map:** collapsing a node also hides the children of its children ([d4a4883](https://github.com/riccardoalv/omini/commit/d4a4883b9825f855e17fe864ac060c97a01b8c02))
* **model:** only parse textual MACs and flag group addresses ([628dcdd](https://github.com/riccardoalv/omini/commit/628dcdd2dcf67ef91e54d3a2ea72e7e0e560baa7))
* **models:** close the response before exiting in the generator ([5157a1f](https://github.com/riccardoalv/omini/commit/5157a1f7fbb93b99861e1aebaae49a2fc049625c))
* **panel:** web interface as an icon next to the device name ([fb9ccdf](https://github.com/riccardoalv/omini/commit/fb9ccdf68cdeb68169d7632ea4c513de6f96df20))
* **plugins:** run plugins with a relative data dir; install by URL from "Add integration" ([bd4afaf](https://github.com/riccardoalv/omini/commit/bd4afaffad4b5fa6c7c71985ddd37f75e92e77a7))
* **snmp:** set a stable device key and make timeouts configurable ([06e7877](https://github.com/riccardoalv/omini/commit/06e78770de131c7b42491e9b9b3a31d68be2828b))
